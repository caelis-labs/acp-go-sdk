package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestConnectionAcceptsJSONRPCBatchAndRepliesAsArray(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return map[string]any{"method": method}, nil
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

	batch := `[{"jsonrpc":"2.0","id":1,"method":"one"},{"jsonrpc":"2.0","id":2,"method":"two"}]` + "\n"
	if _, err := io.WriteString(peerSide, batch); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var replies []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &replies); err != nil {
		t.Fatalf("response %s is not a batch array: %v", line, err)
	}
	if len(replies) != 2 || string(replies[0].ID) != "1" || string(replies[1].ID) != "2" {
		t.Fatalf("replies = %#v", replies)
	}
}

func TestConnectionNotificationOnlyBatchProducesNoResponse(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	seen := make(chan string, 1)
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			seen <- method
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
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, `[{"jsonrpc":"2.0","method":"note"}]`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-seen:
		if method != "note" {
			t.Fatalf("method = %q", method)
		}
	case <-time.After(testTimeout):
		t.Fatal("notification was not dispatched")
	}

	_ = peerSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 16)
	if n, err := peerSide.Read(buf); err == nil {
		t.Fatalf("unexpected response %q", buf[:n])
	}
}

func TestConnectionEmptyBatchReturnsInvalidRequest(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, "[]\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID    json.RawMessage `json:"id"`
		Error *RequestError   `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if string(response.ID) != "null" || response.Error == nil || response.Error.Code != -32600 {
		t.Fatalf("response = %#v", response)
	}
}

func TestConnectionMalformedFrameReturnsParseError(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, "{\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error *RequestError `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != -32700 {
		t.Fatalf("response = %#v", response)
	}
}

func TestConnectionBatchMixedInvalidEntryKeepsSiblings(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return map[string]any{"method": method}, nil
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

	if _, err := io.WriteString(peerSide, `[{"jsonrpc":"2.0","id":1,"method":"ok"},"nope"]`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var replies []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *RequestError   `json:"error"`
	}
	if err := json.Unmarshal(line, &replies); err != nil {
		t.Fatalf("response %s: %v", line, err)
	}
	if len(replies) != 2 || string(replies[0].ID) != "1" || replies[0].Error != nil {
		t.Fatalf("replies = %#v", replies)
	}
	if string(replies[1].ID) != "null" || replies[1].Error == nil || replies[1].Error.Code != -32600 {
		t.Fatalf("invalid entry reply = %#v", replies[1])
	}
}

func TestSendTransportFramePreservesBatchBoundary(t *testing.T) {
	t.Parallel()
	senderSide, receiverSide := net.Pipe()
	sender, err := NewConnectionWithOptions(nil, senderSide, senderSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	defer func() { _ = receiverSide.Close() }()
	if err := receiverSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	raw := []byte(`[{"jsonrpc":"2.0","id":1,"method":"echo"},{"jsonrpc":"2.0","method":"note"}]`)
	frame := ParseTransportFrame(raw)
	if frame.Kind != FrameKindBatch {
		t.Fatalf("kind = %s", frame.Kind)
	}
	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(receiverSide).ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}
		done <- line
	}()
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := sender.SendTransportFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	var line []byte
	select {
	case err := <-errCh:
		t.Fatal(err)
	case line = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if string(line) != string(raw)+"\n" {
		t.Fatalf("relayed %s, want original batch", line)
	}
	var decoded []json.RawMessage
	if err := json.Unmarshal(line, &decoded); err != nil || len(decoded) != 2 {
		t.Fatalf("relayed frame was flattened: %s", line)
	}
}

func TestBatchAfterResponseWaitsForArrayAndAllowsReverseRequest(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	opts := testOptions()
	opts.MaxHandlerConcurrency = 1
	callbacks := make(chan error, 2)
	var connection *Connection
	handler := func(ctx context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if err := AfterResponse(ctx, func(cbCtx context.Context) error {
			if ctx.Err() == nil {
				callbacks <- errors.New("request context is still active")
				return nil
			}
			_, err := SendRequest[any](connection, cbCtx, "reverse", nil)
			callbacks <- err
			return err
		}); err != nil {
			return nil, toReqErr(err)
		}
		return method, nil
	}
	var err error
	connection, err = NewUnstartedConnection(handler, a, a, opts)
	if err != nil {
		t.Fatal(err)
	}
	connection.Start()
	defer func() { _ = connection.Close() }()
	defer func() { _ = b.Close() }()
	if err := b.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(b, `[{"jsonrpc":"2.0","id":1,"method":"one"},{"jsonrpc":"2.0","id":2,"method":"two"}]`+"\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(b)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var replies []anyMessage
	if err := json.Unmarshal(line, &replies); err != nil || len(replies) != 2 {
		t.Fatalf("first frame must be response array: %s (%v)", line, err)
	}
	for range 2 {
		line, err = reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var req anyMessage
		if err := json.Unmarshal(line, &req); err != nil || req.Method != "reverse" {
			t.Fatalf("reverse: %s (%v)", line, err)
		}
		response, err := encodeMessage(anyMessage{ID: req.ID, Result: json.RawMessage(`{}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Write(response); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-callbacks:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(testTimeout):
			t.Fatal("callback stalled")
		}
	}
}

func TestBatchCallbackReservationIsBounded(t *testing.T) {
	t.Parallel()
	input, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	opts := testOptions()
	opts.MaxPendingRequests = 1
	c, err := NewConnectionWithOptions(nil, io.Discard, input, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	first := &afterResponseState{reserve: c.reserveBatchCallback}
	if err := first.add(func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	second := &afterResponseState{reserve: c.reserveBatchCallback}
	if err := second.add(func(context.Context) error { return nil }); !errors.Is(err, ErrAfterResponseQueueFull) {
		t.Fatalf("got %v", err)
	}
}

func TestBatchWriteFailureDoesNotRunCallbacks(t *testing.T) {
	t.Parallel()
	input, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	c, err := NewConnectionWithOptions(nil, failBatchWriter{}, input, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	called := make(chan struct{}, 1)
	b := &batchReply{c: c, remaining: 2, replies: make([]anyMessage, 2), present: make([]bool, 2), completions: make([]responseCompletion, 2)}
	if err := c.reserveBatchCallback(); err != nil {
		t.Fatal(err)
	}
	if err := b.complete(0, anyMessage{Result: json.RawMessage(`{}`)}, responseCompletion{callback: func(context.Context) error { called <- struct{}{}; return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := b.complete(1, anyMessage{Error: NewInvalidRequest(nil)}, responseCompletion{}); err == nil {
		t.Fatal("expected write failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	_ = c.Wait(ctx)
	select {
	case <-called:
		t.Fatal("callback ran after failed write")
	default:
	}
	if len(c.batchCallbackSlots) != 0 {
		t.Fatal("callback reservation leaked")
	}
}

type failBatchWriter struct{}

func (failBatchWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestBatchRetainsRequestIDsUntilResponseIsWritten(t *testing.T) {
	t.Parallel()
	a, b := net.Pipe()
	slowStarted, releaseSlow, fastFinished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		switch method {
		case "fast":
			close(fastFinished)
		case "slow":
			close(slowStarted)
			<-releaseSlow
		case "duplicate":
			return "unexpected", nil
		}
		return method, nil
	}
	opts := testOptions()
	opts.MaxHandlerConcurrency = 2
	c, err := NewConnectionWithOptions(handler, a, a, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	defer func() { _ = b.Close() }()
	defer close(releaseSlow)
	if err := b.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(b, `[{"jsonrpc":"2.0","id":1,"method":"fast"},{"jsonrpc":"2.0","id":2,"method":"slow"}]`+"\n"); err != nil {
		t.Fatal(err)
	}
	<-slowStarted
	<-fastFinished
	// Wait for the first worker to finish its collector submission by queuing
	// a probe; the slow worker remains occupied. Exactly two workers are used.
	if _, err := io.WriteString(b, `{"jsonrpc":"2.0","id":3,"method":"probe"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(b)
	if _, err := reader.ReadBytes('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(b, `{"jsonrpc":"2.0","id":1,"method":"duplicate"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var reply anyMessage
	if err := json.Unmarshal(line, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error == nil || reply.Error.Code != -32600 {
		t.Fatalf("duplicate admitted before flush: %s", line)
	}
}
