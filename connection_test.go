package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

func testOptions() ConnectionOptions {
	return ConnectionOptions{
		MaxFrameSize:           64 * 1024,
		MaxPendingRequests:     16,
		MaxHandlerConcurrency:  4,
		MaxQueuedRequests:      16,
		MaxQueuedNotifications: 32,
		MaxQueuedWrites:        32,
	}
}

func waitContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), testTimeout)
}

func TestContextTerminationMapsToRequestCancelled(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		requestErr := toReqErr(err)
		if requestErr.Code != -32800 {
			t.Fatalf("toReqErr(%v) = %#v, want request cancelled", err, requestErr)
		}
	}
}

func TestConnectionPreservesStringLargeIntegerAndNullIDs(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, _ string, _ json.RawMessage) (any, *RequestError) {
			return map[string]any{"ok": true}, nil
		},
		connectionSide,
		connectionSide,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(peerSide)
	for _, rawID := range []string{`"request-α"`, "9223372036854775807", "null"} {
		request := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"method":"echo","params":{}}`+"\n", rawID)
		if _, err := io.WriteString(peerSide, request); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(line, &response); err != nil {
			t.Fatal(err)
		}
		if got := string(response.ID); got != rawID {
			t.Fatalf("response id = %s, want %s", got, rawID)
		}
	}
}

func TestNotificationHandlerCanSendReverseRequest(t *testing.T) {
	t.Parallel()
	leftTransport, rightTransport := net.Pipe()
	handled := make(chan string, 1)
	progressHandled := make(chan struct{})
	var left *Connection
	var err error
	left, err = NewConnectionWithOptions(
		func(ctx context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			switch method {
			case "progress":
				close(progressHandled)
				return nil, nil
			case "notify":
			default:
				return nil, NewMethodNotFound(method)
			}
			response, requestErr := SendRequest[string](left, ctx, "reverse", nil)
			if requestErr != nil {
				return nil, toReqErr(requestErr)
			}
			select {
			case <-progressHandled:
				handled <- response
			default:
				return nil, NewInternalError(map[string]any{"error": "response returned before progress notification"})
			}
			return nil, nil
		},
		leftTransport,
		leftTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	var right *Connection
	right, err = NewConnectionWithOptions(
		func(ctx context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			if method != "reverse" {
				return nil, NewMethodNotFound(method)
			}
			if err := right.SendNotification(ctx, "progress", nil); err != nil {
				return nil, toReqErr(err)
			}
			return "reverse-ok", nil
		},
		rightTransport,
		rightTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()

	ctx, cancel := waitContext(t)
	defer cancel()
	if err := right.SendNotification(ctx, "notify", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-handled:
		if got != "reverse-ok" {
			t.Fatalf("reverse response = %q", got)
		}
	case <-ctx.Done():
		t.Fatal("notification handler deadlocked waiting for its own completion barrier")
	}
}

func TestNotificationHandlerContextExpiresOnReturn(t *testing.T) {
	t.Parallel()
	leftTransport, rightTransport := net.Pipe()
	releaseAsync := make(chan struct{})
	asyncDone := make(chan error, 1)
	var left *Connection
	var err error
	left, err = NewConnectionWithOptions(
		func(ctx context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			switch method {
			case "notify-async":
				go func() {
					<-releaseAsync
					_, requestErr := SendRequest[string](left, ctx, "late-reverse", nil)
					asyncDone <- requestErr
				}()
				return nil, nil
			case "ping":
				return "pong", nil
			default:
				return nil, NewMethodNotFound(method)
			}
		},
		leftTransport,
		leftTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewConnectionWithOptions(nil, rightTransport, rightTransport, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()

	ctx, cancel := waitContext(t)
	defer cancel()
	if err := right.SendNotification(ctx, "notify-async", nil); err != nil {
		t.Fatal(err)
	}
	for !left.notificationAlreadyProcessed(1) {
		select {
		case <-ctx.Done():
			t.Fatal("notification handler did not return")
		case <-time.After(time.Millisecond):
		}
	}
	close(releaseAsync)
	select {
	case err := <-asyncDone:
		var requestErr *RequestError
		if !errors.As(err, &requestErr) || requestErr.Code != -32800 {
			t.Fatalf("late reverse request error = %v, want cancellation", err)
		}
	case <-ctx.Done():
		t.Fatal("late reverse request did not observe expired handler context")
	}

	if got, err := SendRequest[string](right, ctx, "ping", nil); err != nil || got != "pong" {
		t.Fatalf("connection after late request = %q, %v", got, err)
	}
}

func TestConnectionSupportsConcurrentBidirectionalRequests(t *testing.T) {
	t.Parallel()
	leftTransport, rightTransport := net.Pipe()
	left, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return "left handled " + method, nil
		},
		leftTransport,
		leftTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return "right handled " + method, nil
		},
		rightTransport,
		rightTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()

	ctx, cancel := waitContext(t)
	defer cancel()
	type result struct {
		value string
		err   error
	}
	results := make(chan result, 2)
	go func() {
		value, err := SendRequest[string](left, ctx, "to-right", map[string]any{"side": "left"})
		results <- result{value: value, err: err}
	}()
	go func() {
		value, err := SendRequest[string](right, ctx, "to-left", map[string]any{"side": "right"})
		results <- result{value: value, err: err}
	}()

	seen := map[string]bool{}
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		seen[result.value] = true
	}
	if !seen["right handled to-right"] || !seen["left handled to-left"] {
		t.Fatalf("unexpected results: %#v", seen)
	}
}

func TestConnectionBoundsHandlerConcurrency(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	var active atomic.Int32
	var maximum atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	opts := testOptions()
	opts.MaxHandlerConcurrency = 2
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, _ string, _ json.RawMessage) (any, *RequestError) {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			return nil, nil
		},
		connectionSide,
		connectionSide,
		opts,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	var requests strings.Builder
	for id := 1; id <= 4; id++ {
		fmt.Fprintf(&requests, `{"jsonrpc":"2.0","id":%d,"method":"block"}`+"\n", id)
	}
	if _, err := io.WriteString(peerSide, requests.String()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(testTimeout):
			t.Fatal("handlers did not start")
		}
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum handler concurrency = %d, want 2", got)
	}
	select {
	case <-started:
		t.Fatal("third handler started before capacity was released")
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	reader := bufio.NewReader(peerSide)
	for range 4 {
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
	}
	if got := maximum.Load(); got > 2 {
		t.Fatalf("maximum handler concurrency = %d, want <= 2", got)
	}
}

func TestConnectionPreservesNotificationOrder(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	const count = 20
	var mu sync.Mutex
	seen := make([]int, 0, count)
	complete := make(chan struct{})
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, _ string, params json.RawMessage) (any, *RequestError) {
			var value int
			if err := json.Unmarshal(params, &value); err != nil {
				return nil, NewInvalidParams(nil)
			}
			mu.Lock()
			seen = append(seen, value)
			if len(seen) == count {
				close(complete)
			}
			mu.Unlock()
			return nil, nil
		},
		connectionSide,
		connectionSide,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	var notifications strings.Builder
	for i := range count {
		fmt.Fprintf(&notifications, `{"jsonrpc":"2.0","method":"note","params":%d}`+"\n", i)
	}
	if _, err := io.WriteString(peerSide, notifications.String()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-complete:
	case <-time.After(testTimeout):
		t.Fatal("notifications were not processed")
	}
	mu.Lock()
	defer mu.Unlock()
	for i, value := range seen {
		if value != i {
			t.Fatalf("notification %d = %d", i, value)
		}
	}
}

func TestResponseWaitsForEarlierNotifications(t *testing.T) {
	t.Parallel()
	leftTransport, rightTransport := net.Pipe()
	notificationStarted := make(chan struct{})
	releaseNotification := make(chan struct{})
	var seen []int
	var seenMu sync.Mutex
	left, err := NewConnectionWithOptions(
		func(_ context.Context, _ string, params json.RawMessage) (any, *RequestError) {
			var value int
			if err := json.Unmarshal(params, &value); err != nil {
				return nil, NewInvalidParams(nil)
			}
			if value == 0 {
				close(notificationStarted)
				<-releaseNotification
			}
			seenMu.Lock()
			seen = append(seen, value)
			seenMu.Unlock()
			return nil, nil
		},
		leftTransport,
		leftTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()

	var right *Connection
	right, err = NewConnectionWithOptions(
		func(ctx context.Context, _ string, _ json.RawMessage) (any, *RequestError) {
			for i := range 3 {
				if err := right.SendNotification(ctx, "progress", i); err != nil {
					return nil, toReqErr(err)
				}
			}
			return "done", nil
		},
		rightTransport,
		rightTransport,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = right.Close() }()

	ctx, cancel := waitContext(t)
	defer cancel()
	requestDone := make(chan error, 1)
	go func() {
		_, err := SendRequest[string](left, ctx, "turn", nil)
		requestDone <- err
	}()
	select {
	case <-notificationStarted:
	case <-time.After(testTimeout):
		t.Fatal("notification handler did not start")
	}
	select {
	case err := <-requestDone:
		t.Fatalf("request returned before notification barrier: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseNotification)
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testTimeout):
		t.Fatal("request did not complete")
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	if fmt.Sprint(seen) != "[0 1 2]" {
		t.Fatalf("notification order = %v", seen)
	}
}

type blockingWriter struct {
	release <-chan struct{}
	started chan<- struct{}
}

func (w blockingWriter) Write(p []byte) (int, error) {
	select {
	case w.started <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

func TestWriteQueueIsBounded(t *testing.T) {
	t.Parallel()
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	opts := testOptions()
	opts.MaxQueuedWrites = 1
	connection, err := NewConnectionWithOptions(nil, blockingWriter{release: release, started: started}, readSide, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- connection.SendNotification(context.Background(), "first", nil) }()
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("first write did not start")
	}
	go func() { secondDone <- connection.SendNotification(context.Background(), "second", nil) }()
	queueDeadline := time.NewTimer(testTimeout)
	defer queueDeadline.Stop()
	for len(connection.writeQueue) != 1 {
		select {
		case <-queueDeadline.C:
			t.Fatal("second write did not enter bounded queue")
		case <-time.After(time.Millisecond):
		}
	}

	deadlineCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := connection.SendNotification(deadlineCtx, "third", nil); !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatalf("third notification error = %v, want deadline exceeded", err)
	}
	close(release)
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(testTimeout):
			t.Fatal("queued write did not complete")
		}
	}
}

func TestPromptCancellationReturnsUnderWriteBackpressure(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	opts := testOptions()
	opts.MaxQueuedWrites = 1
	client, err := NewClientSideConnectionWithOptions(nil, blockingWriter{release: release, started: started}, readSide, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go func() { firstDone <- client.conn.SendNotification(context.Background(), "first", nil) }()
	select {
	case <-started:
	case <-time.After(testTimeout):
		close(release)
		t.Fatal("first write did not start")
	}
	go func() { secondDone <- client.conn.SendNotification(context.Background(), "second", nil) }()
	queueDeadline := time.NewTimer(testTimeout)
	defer queueDeadline.Stop()
	for len(client.conn.writeQueue) != 1 {
		select {
		case <-queueDeadline.C:
			close(release)
			t.Fatal("second write did not enter bounded queue")
		case <-time.After(time.Millisecond):
		}
	}

	promptCtx, cancelPrompt := context.WithCancel(context.Background())
	cancelPrompt()
	promptDone := make(chan error, 1)
	go func() {
		_, promptErr := client.Prompt(promptCtx, PromptRequest{SessionId: "s", Prompt: []ContentBlock{}})
		promptDone <- promptErr
	}()
	var promptErr error
	select {
	case promptErr = <-promptDone:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("Prompt did not return under write backpressure")
	}
	var requestErr *RequestError
	if !errors.As(promptErr, &requestErr) || requestErr.Code != -32800 {
		close(release)
		t.Fatalf("Prompt error = %v, want JSON-RPC request cancelled", promptErr)
	}

	close(release)
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(testTimeout):
			t.Fatal("backpressured write did not complete")
		}
	}
}

func TestCancelRequestCancelsInboundHandler(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	started := make(chan struct{})
	connection, err := NewConnectionWithOptions(
		func(ctx context.Context, _ string, _ json.RawMessage) (any, *RequestError) {
			close(started)
			<-ctx.Done()
			return nil, toReqErr(ctx.Err())
		},
		connectionSide,
		connectionSide,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, `{"jsonrpc":"2.0","id":"cancel-me","method":"block"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("handler did not start")
	}
	if _, err := io.WriteString(peerSide, `{"jsonrpc":"2.0","method":"$/cancel_request","params":{"requestId":"cancel-me"}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response anyMessage
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != -32800 {
		t.Fatalf("response error = %#v, want -32800", response.Error)
	}
}

func TestPendingRequestLimit(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	opts := testOptions()
	opts.MaxPendingRequests = 1
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	firstRead := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(peerSide)
		if scanner.Scan() {
			close(firstRead)
		}
		for scanner.Scan() {
		}
	}()

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := SendRequest[json.RawMessage](connection, firstCtx, "first", nil)
		firstDone <- err
	}()
	select {
	case <-firstRead:
	case <-time.After(testTimeout):
		t.Fatal("first request was not written")
	}

	ctx, cancel := waitContext(t)
	defer cancel()
	if _, err := SendRequest[json.RawMessage](connection, ctx, "second", nil); !errors.Is(err, ErrPendingRequestsExceeded) {
		t.Fatalf("second request error = %v, want %v", err, ErrPendingRequestsExceeded)
	}
	cancelFirst()
	select {
	case err := <-firstDone:
		var requestErr *RequestError
		if !errors.As(err, &requestErr) || requestErr.Code != -32800 {
			t.Fatalf("first request error = %v, want cancellation RequestError", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("first request did not cancel")
	}
}

func TestFrameLimitClosesConnection(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	opts := testOptions()
	opts.MaxFrameSize = 64
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetWriteDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}
	frame := `{"jsonrpc":"2.0","method":"oversized","params":"` + strings.Repeat("x", 128) + `"}` + "\n"
	_, _ = io.WriteString(peerSide, frame)
	select {
	case <-connection.Done():
	case <-time.After(testTimeout):
		t.Fatal("connection did not close")
	}
	if !errors.Is(connection.Err(), ErrFrameTooLarge) {
		t.Fatalf("connection error = %v, want %v", connection.Err(), ErrFrameTooLarge)
	}
}

func TestCloseAndWaitAreDeterministic(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peerSide.Close() }()

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := connection.Wait(ctx); !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("Wait error = %v, want %v", err, ErrConnectionClosed)
	}
	if !errors.Is(connection.Err(), ErrConnectionClosed) {
		t.Fatalf("Err = %v, want %v", connection.Err(), ErrConnectionClosed)
	}
}

func TestWrappedRequestErrorSurvivesErrorsAs(t *testing.T) {
	t.Parallel()
	want := NewInvalidParams(map[string]any{"field": "value"})
	got := toReqErr(fmt.Errorf("wrapped: %w", want))
	var requestErr *RequestError
	if !errors.As(got, &requestErr) {
		t.Fatal("errors.As did not find RequestError")
	}
	if requestErr.Code != want.Code || requestErr.Message != want.Message {
		t.Fatalf("RequestError = %#v, want %#v", requestErr, want)
	}
}
