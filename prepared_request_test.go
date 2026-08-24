package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPreparedRequestCancellationBeforeWriterAdmissionDoesNotWrite(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	writer := newLifecycleBlockingWriter()
	connection, err := NewConnectionWithOptions(nil, writer, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- connection.SendNotification(context.Background(), "first", nil)
	}()
	select {
	case <-writer.started:
	case <-time.After(testTimeout):
		t.Fatal("first write did not start")
	}

	request, err := PrepareRequest[json.RawMessage](connection, "queued", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var abortCalls atomic.Int32
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- request.Dispatch(ctx, DispatchOptions{Abort: func(error) error {
			abortCalls.Add(1)
			return nil
		}})
	}()
	waitForWriteQueueLength(t, connection, 1)
	cancel()

	select {
	case err := <-dispatchDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Dispatch error = %v, want context cancellation", err)
		}
		state, ok := RequestSubmissionStateOf(err)
		if !ok || state != RequestSubmissionNotStarted || RequestMayHaveBeenSubmitted(err) {
			t.Fatalf("submission = %v ok=%v may=%v", state, ok, RequestMayHaveBeenSubmitted(err))
		}
	case <-time.After(testTimeout):
		t.Fatal("Dispatch did not return after pre-write cancellation")
	}
	if got := abortCalls.Load(); got != 0 {
		t.Fatalf("Abort calls = %d, want 0", got)
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}

	writer.releaseWrite()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	deadline := time.After(testTimeout)
	for writer.callCount() != 1 {
		select {
		case <-deadline:
			t.Fatalf("writer calls = %d, want 1", writer.callCount())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestPreparedRequestLiveOperationsAreNotClassifiedSafeToRetry(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	writer := newLifecycleBlockingWriter()
	connection, err := NewConnectionWithOptions(nil, writer, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- connection.SendNotification(context.Background(), "first", nil)
	}()
	select {
	case <-writer.started:
	case <-time.After(testTimeout):
		t.Fatal("first write did not start")
	}

	request, err := PrepareRequest[json.RawMessage](connection, "queued", nil)
	if err != nil {
		t.Fatal(err)
	}
	assertPending := func(operation string, err error) {
		t.Helper()
		state, ok := RequestSubmissionStateOf(err)
		if !ok || state != RequestSubmissionPending || !RequestMayHaveBeenSubmitted(err) {
			t.Fatalf("%s error = %v, submission=%v ok=%v may=%v", operation, err, state, ok, RequestMayHaveBeenSubmitted(err))
		}
	}
	_, err = request.Wait(context.Background())
	assertPending("Wait before Dispatch", err)

	dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- request.Dispatch(dispatchCtx, DispatchOptions{})
	}()
	waitForWriteQueueLength(t, connection, 1)
	assertPending("duplicate Dispatch", request.Dispatch(context.Background(), DispatchOptions{}))
	assertPending("CancelRequest during Dispatch", request.CancelRequest(context.Background()))
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWait()
	_, err = request.Wait(waitCtx)
	assertPending("Wait during Dispatch", err)

	cancelDispatch()
	if err := <-dispatchDone; err == nil {
		t.Fatal("Dispatch succeeded after cancellation")
	} else if state, ok := RequestSubmissionStateOf(err); !ok || state != RequestSubmissionNotStarted {
		t.Fatalf("terminal Dispatch submission = %v ok=%v err=%v", state, ok, err)
	}
	writer.releaseWrite()
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
}

func TestRequestSubmissionClassificationCannotBeForged(t *testing.T) {
	fakeConcrete := &RequestLifecycleError{}
	if state, ok := RequestSubmissionStateOf(fakeConcrete); ok || state != RequestSubmissionNotStarted {
		t.Fatalf("zero RequestLifecycleError classified as %v ok=%v", state, ok)
	}
	if !RequestMayHaveBeenSubmitted(fakeConcrete) {
		t.Fatal("unclassified concrete error was treated as safe to retry")
	}
	fakeInterface := fakeSubmissionError{}
	if _, ok := RequestSubmissionStateOf(fakeInterface); ok {
		t.Fatal("arbitrary SubmissionState implementation was accepted")
	}
	if !RequestMayHaveBeenSubmitted(fakeInterface) {
		t.Fatal("arbitrary error was treated as safe to retry")
	}
}

func TestPreparedRequestStartedWriteCancellationAbortsOnce(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	writer := newLifecycleBlockingWriter()
	connection, err := NewConnectionWithOptions(nil, writer, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	request, err := PrepareRequest[json.RawMessage](connection, "started", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var abortCalls atomic.Int32
	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- request.Dispatch(ctx, DispatchOptions{Abort: func(error) error {
			abortCalls.Add(1)
			writer.releaseWrite()
			return nil
		}})
	}()
	select {
	case <-writer.started:
	case <-time.After(testTimeout):
		t.Fatal("prepared request write did not start")
	}
	cancel()
	err = <-dispatchDone
	if !errors.Is(err, context.Canceled) || !RequestMayHaveBeenSubmitted(err) {
		t.Fatalf("Dispatch error = %v may=%v", err, RequestMayHaveBeenSubmitted(err))
	}
	if state, ok := RequestSubmissionStateOf(err); !ok || state != RequestSubmissionPossible {
		t.Fatalf("submission = %v ok=%v", state, ok)
	}
	if got := abortCalls.Load(); got != 1 {
		t.Fatalf("Abort calls = %d, want 1", got)
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
}

func TestPreparedRequestAdmissionRechecksCancellationAndConnectionClose(t *testing.T) {
	tests := []struct {
		name      string
		terminate func(context.CancelFunc, *Connection)
		wantErr   error
	}{
		{
			name: "dispatch cancellation",
			terminate: func(cancel context.CancelFunc, _ *Connection) {
				cancel()
			},
			wantErr: context.Canceled,
		},
		{
			name: "connection close",
			terminate: func(_ context.CancelFunc, connection *Connection) {
				if err := connection.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			},
			wantErr: ErrConnectionClosed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readSide, keepOpen := io.Pipe()
			defer func() { _ = keepOpen.Close() }()
			writer := newLifecycleBlockingWriter()
			connection, err := NewConnectionWithOptions(nil, writer, readSide, testOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()

			firstDone := make(chan error, 1)
			go func() {
				firstDone <- connection.SendNotification(context.Background(), "first", nil)
			}()
			select {
			case <-writer.started:
			case <-time.After(testTimeout):
				t.Fatal("first write did not start")
			}

			request, err := PrepareRequest[json.RawMessage](connection, "queued", nil)
			if err != nil {
				t.Fatal(err)
			}
			dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
			defer cancelDispatch()
			dispatchDone := make(chan error, 1)
			go func() {
				dispatchDone <- request.Dispatch(dispatchCtx, DispatchOptions{})
			}()
			waitForWriteQueueLength(t, connection, 1)

			// Hold the request lock so processWrites can pass its initial context
			// check but cannot cross the request's writer-admission point.
			request.mu.Lock()
			writer.releaseWrite()
			waitForWriteQueueLength(t, connection, 0)
			test.terminate(cancelDispatch, connection)
			request.mu.Unlock()

			select {
			case err := <-dispatchDone:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Dispatch error = %v, want %v", err, test.wantErr)
				}
				if state, ok := RequestSubmissionStateOf(err); !ok || state != RequestSubmissionNotStarted {
					t.Fatalf("submission = %v ok=%v err=%v", state, ok, err)
				}
			case <-time.After(testTimeout):
				t.Fatal("Dispatch did not finish")
			}
			if got := writer.callCount(); got != 1 {
				t.Fatalf("writer calls = %d, want no request write after termination", got)
			}
			select {
			case err := <-firstDone:
				if err != nil && !errors.Is(err, ErrConnectionClosed) {
					t.Fatalf("first notification: %v", err)
				}
			case <-time.After(testTimeout):
				t.Fatal("first notification did not finish")
			}
		})
	}
}

func TestPreparedRequestWriteFailuresArePossiblySubmitted(t *testing.T) {
	writeErr := errors.New("write failed")
	tests := []struct {
		name   string
		writer io.Writer
	}{
		{name: "zero byte", writer: lifecycleFixedWriter{}},
		{name: "zero byte error", writer: lifecycleFixedWriter{err: writeErr}},
		{name: "partial write", writer: lifecycleFixedWriter{written: 1, err: writeErr}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readSide, keepOpen := io.Pipe()
			defer func() { _ = keepOpen.Close() }()
			connection, err := NewConnectionWithOptions(nil, test.writer, readSide, testOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			request, err := PrepareRequest[json.RawMessage](connection, "failure", nil)
			if err != nil {
				t.Fatal(err)
			}
			var abortCalls atomic.Int32
			err = request.Dispatch(context.Background(), DispatchOptions{Abort: func(error) error {
				abortCalls.Add(1)
				return nil
			}})
			if err == nil || !RequestMayHaveBeenSubmitted(err) {
				t.Fatalf("Dispatch error = %v may=%v", err, RequestMayHaveBeenSubmitted(err))
			}
			if got := abortCalls.Load(); got != 1 {
				t.Fatalf("Abort calls = %d, want 1", got)
			}
			if got := pendingRequestCount(connection); got != 0 {
				t.Fatalf("pending requests = %d, want 0", got)
			}
		})
	}
}

func TestPreparedRequestWaitUsesIndependentContext(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	releaseResponse := make(chan struct{})
	go func() {
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var request anyMessage
		if json.Unmarshal(line, &request) != nil {
			return
		}
		<-releaseResponse
		response := anyMessage{ID: request.ID, Result: json.RawMessage(`"done"`)}
		encoded, encodeErr := encodeMessage(response)
		if encodeErr == nil {
			_, _ = peerSide.Write(encoded)
		}
	}()

	request, err := PrepareRequest[string](connection, "transfer", nil)
	if err != nil {
		t.Fatal(err)
	}
	dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
	if err := request.Dispatch(dispatchCtx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	cancelDispatch()
	close(releaseResponse)
	waitCtx, cancelWait := waitContext(t)
	defer cancelWait()
	result, err := request.Wait(waitCtx)
	if err != nil || result != "done" {
		t.Fatalf("Wait = %q, %v", result, err)
	}
}

func TestPreparedRequestIgnoresResponseBeforeWriterAdmission(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	earlyIngressHandled := make(chan struct{})
	connection, err := NewConnectionWithOptions(func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if method == "early-response-barrier" {
			close(earlyIngressHandled)
			return nil, nil
		}
		return nil, NewMethodNotFound(method)
	}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	request, err := PrepareRequest[string](connection, "admitted-response", nil)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		earlyID := json.RawMessage("1")
		early, _ := encodeMessage(anyMessage{ID: &earlyID, Result: json.RawMessage(`"spoofed"`)})
		_, _ = peerSide.Write(early)
		barrier, _ := encodeMessage(anyMessage{Method: "early-response-barrier"})
		_, _ = peerSide.Write(barrier)
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var outbound anyMessage
		if json.Unmarshal(line, &outbound) != nil {
			return
		}
		actual, _ := encodeMessage(anyMessage{ID: outbound.ID, Result: json.RawMessage(`"actual"`)})
		_, _ = peerSide.Write(actual)
	}()
	select {
	case <-earlyIngressHandled:
	case <-time.After(testTimeout):
		t.Fatal("early response was not ingested")
	}
	if err := request.Dispatch(context.Background(), DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	result, err := request.Wait(ctx)
	if err != nil || result != "actual" {
		t.Fatalf("Wait = %q, %v; early response was accepted", result, err)
	}
}

func TestPreparedRequestObserverCausalOrdering(t *testing.T) {
	leftTransport, rightTransport := net.Pipe()
	beforeStarted := make(chan struct{})
	releaseBefore := make(chan struct{})
	afterHandled := make(chan bool, 1)
	var observerComplete atomic.Bool

	left, err := NewConnectionWithOptions(func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		switch method {
		case "before":
			close(beforeStarted)
			<-releaseBefore
		case "after":
			afterHandled <- observerComplete.Load()
		default:
			return nil, NewMethodNotFound(method)
		}
		return nil, nil
	}, leftTransport, leftTransport, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	var right *Connection
	right, err = NewConnectionWithOptions(func(ctx context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if method != "ordered" {
			return nil, NewMethodNotFound(method)
		}
		if err := right.SendNotification(ctx, "before", nil); err != nil {
			return nil, toReqErr(err)
		}
		if err := AfterResponse(ctx, func(callbackCtx context.Context) error {
			return right.SendNotification(callbackCtx, "after", nil)
		}); err != nil {
			return nil, toReqErr(err)
		}
		return "ok", nil
	}, rightTransport, rightTransport, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()

	request, err := PrepareRequest[string](left, "ordered", nil)
	if err != nil {
		t.Fatal(err)
	}
	observerEntered := make(chan struct{})
	releaseObserver := make(chan struct{})
	observerErr := errors.New("observer rejected response")
	if err := request.ObserveResponse(func(_ context.Context, response RPCResponse) error {
		if response.Error != nil || string(response.Result) != `"ok"` {
			t.Errorf("observed response = %#v", response)
		}
		close(observerEntered)
		<-releaseObserver
		observerComplete.Store(true)
		return observerErr
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := request.Wait(ctx)
		waitDone <- waitErr
	}()
	select {
	case <-beforeStarted:
	case <-ctx.Done():
		t.Fatal("earlier notification did not start")
	}
	select {
	case <-observerEntered:
		t.Fatal("observer ran before earlier notification completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseBefore)
	select {
	case <-observerEntered:
	case <-ctx.Done():
		t.Fatal("observer did not run")
	}
	select {
	case <-afterHandled:
		t.Fatal("later notification crossed response observer")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseObserver)
	if err := <-waitDone; !errors.Is(err, observerErr) {
		t.Fatalf("Wait error = %v, want observer error", err)
	}
	select {
	case complete := <-afterHandled:
		if !complete {
			t.Fatal("later notification observed incomplete observer")
		}
	case <-ctx.Done():
		t.Fatal("later notification was not released")
	}
}

func TestPreparedRequestObserverSeesJSONRPCError(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	go respondToOneRequest(peerSide, func(request anyMessage) anyMessage {
		return anyMessage{ID: request.ID, Error: NewAuthRequired(map[string]any{"method": "oauth"})}
	})
	request, err := PrepareRequest[json.RawMessage](connection, "authenticate", nil)
	if err != nil {
		t.Fatal(err)
	}
	observerCalled := false
	if err := request.ObserveResponse(func(_ context.Context, response RPCResponse) error {
		observerCalled = true
		if response.Error == nil || response.Error.Code != -32000 {
			t.Fatalf("observed error = %#v", response.Error)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = request.Wait(ctx)
	var requestErr *RequestError
	if !observerCalled || !errors.As(err, &requestErr) || requestErr.Code != -32000 {
		t.Fatalf("Wait error = %v observerCalled=%v", err, observerCalled)
	}
	if !RequestMayHaveBeenSubmitted(err) {
		t.Fatal("peer error lost submission classification")
	}
}

func TestPreparedRequestObserverCannotMutateWaitError(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	go respondToOneRequest(peerSide, func(request anyMessage) anyMessage {
		return anyMessage{ID: request.ID, Error: NewAuthRequired(map[string]any{"method": "oauth"})}
	})
	request, err := PrepareRequest[json.RawMessage](connection, "authenticate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := request.ObserveResponse(func(_ context.Context, response RPCResponse) error {
		data, ok := response.Error.Data.(map[string]any)
		if !ok {
			t.Fatalf("observed error data = %#v", response.Error.Data)
		}
		data["method"] = "mutated"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = request.Wait(ctx)
	var requestErr *RequestError
	if !errors.As(err, &requestErr) {
		t.Fatalf("Wait error = %v", err)
	}
	data, ok := requestErr.Data.(map[string]any)
	if !ok || data["method"] != "oauth" {
		t.Fatalf("Wait error data = %#v, want original", requestErr.Data)
	}
}

func TestPreparedRequestDecodeFailureIsPossiblySubmitted(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	go respondToOneRequest(peerSide, func(request anyMessage) anyMessage {
		return anyMessage{ID: request.ID, Result: json.RawMessage(`{"outcome":42}`)}
	})
	type response struct {
		Outcome string `json:"outcome"`
	}
	request, err := PrepareRequest[response](connection, "decode", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = request.Wait(ctx)
	var decodeErr *ResponseDecodeError
	if !errors.As(err, &decodeErr) || !RequestMayHaveBeenSubmitted(err) {
		t.Fatalf("Wait error = %v may=%v", err, RequestMayHaveBeenSubmitted(err))
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
}

func TestPreparedRequestAbandonIsIdempotent(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	var writer bytes.Buffer
	connection, err := NewConnectionWithOptions(nil, &writer, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	request, err := PrepareRequest[json.RawMessage](connection, "abandon", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first, second := request.Abandon(), request.Abandon(); first != RequestSubmissionNotStarted || second != first {
		t.Fatalf("Abandon states = %v, %v", first, second)
	}
	if writer.Len() != 0 || pendingRequestCount(connection) != 0 {
		t.Fatalf("writer bytes = %d pending = %d", writer.Len(), pendingRequestCount(connection))
	}
	if _, err := request.Wait(context.Background()); !errors.Is(err, ErrRequestAbandoned) {
		t.Fatalf("Wait error = %v, want abandoned", err)
	}

	dispatched, err := PrepareRequest[json.RawMessage](connection, "dispatched", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatched.Dispatch(context.Background(), DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	if state := dispatched.Abandon(); state != RequestSubmissionPossible {
		t.Fatalf("dispatched Abandon state = %v", state)
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
}

func TestPreparedRequestAbandonWhileObserverIsResolving(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	observerComplete := atomic.Bool{}
	afterHandled := make(chan bool, 1)
	connection, err := NewConnectionWithOptions(func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if method == "after" {
			afterHandled <- observerComplete.Load()
			return nil, nil
		}
		return nil, NewMethodNotFound(method)
	}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	go func() {
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var request anyMessage
		if json.Unmarshal(line, &request) != nil {
			return
		}
		response, _ := encodeMessage(anyMessage{ID: request.ID, Result: json.RawMessage(`"ok"`)})
		after, _ := encodeMessage(anyMessage{Method: "after"})
		_, _ = peerSide.Write(response)
		_, _ = peerSide.Write(after)
	}()

	request, err := PrepareRequest[string](connection, "abandon-resolving", nil)
	if err != nil {
		t.Fatal(err)
	}
	observerEntered := make(chan struct{})
	releaseObserver := make(chan struct{})
	if err := request.ObserveResponse(func(context.Context, RPCResponse) error {
		close(observerEntered)
		<-releaseObserver
		observerComplete.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := request.Wait(ctx)
		waitDone <- waitErr
	}()
	select {
	case <-observerEntered:
	case <-ctx.Done():
		t.Fatal("observer did not start")
	}
	if state := request.Abandon(); state != RequestSubmissionPossible {
		t.Fatalf("Abandon state = %v", state)
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
	select {
	case <-afterHandled:
		t.Fatal("later notification crossed a running observer")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseObserver)
	if err := <-waitDone; !errors.Is(err, ErrRequestAbandoned) {
		t.Fatalf("Wait error = %v, want abandoned", err)
	}
	select {
	case complete := <-afterHandled:
		if !complete {
			t.Fatal("later notification observed incomplete observer")
		}
	case <-ctx.Done():
		t.Fatal("later notification was not released")
	}
}

func TestPreparedRequestConnectionCloseDoesNotCrossRunningObserver(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	afterHandled := make(chan struct{}, 1)
	connection, err := NewConnectionWithOptions(func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if method == "after" {
			afterHandled <- struct{}{}
			return nil, nil
		}
		return nil, NewMethodNotFound(method)
	}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	go func() {
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var request anyMessage
		if json.Unmarshal(line, &request) != nil {
			return
		}
		response, _ := encodeMessage(anyMessage{ID: request.ID, Result: json.RawMessage(`"ok"`)})
		after, _ := encodeMessage(anyMessage{Method: "after"})
		_, _ = peerSide.Write(response)
		_, _ = peerSide.Write(after)
	}()

	request, err := PrepareRequest[string](connection, "close-observer", nil)
	if err != nil {
		t.Fatal(err)
	}
	observerEntered := make(chan struct{})
	releaseObserver := make(chan struct{})
	if err := request.ObserveResponse(func(context.Context, RPCResponse) error {
		close(observerEntered)
		<-releaseObserver
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan error, 1)
	go func() {
		_, waitErr := request.Wait(ctx)
		waitDone <- waitErr
	}()
	select {
	case <-observerEntered:
	case <-ctx.Done():
		t.Fatal("observer did not start")
	}
	waitForEnqueuedNotificationSeq(t, connection, 1)
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-afterHandled:
		t.Fatal("connection close released a later notification into its handler")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseObserver)
	if err := <-waitDone; err != nil {
		t.Fatalf("Wait error after accepted response = %v", err)
	}
	select {
	case <-afterHandled:
		t.Fatal("closed connection dispatched a queued notification")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestPreparedRequestConnectionCloseTerminatesPending(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	connection, err := NewConnectionWithOptions(nil, io.Discard, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	request, err := PrepareRequest[json.RawMessage](connection, "close", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Dispatch(context.Background(), DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = request.Wait(context.Background())
	if !errors.Is(err, ErrConnectionClosed) || !RequestMayHaveBeenSubmitted(err) {
		t.Fatalf("Wait error = %v may=%v", err, RequestMayHaveBeenSubmitted(err))
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
}

func TestPreparedRequestConnectionCloseTerminatesUndispatchedPending(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	connection, err := NewConnectionWithOptions(nil, io.Discard, readSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	request, err := PrepareRequest[json.RawMessage](connection, "close-before-dispatch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = request.Wait(context.Background())
	if !errors.Is(err, ErrConnectionClosed) {
		t.Fatalf("Wait error = %v, want connection close", err)
	}
	if state, ok := RequestSubmissionStateOf(err); !ok || state != RequestSubmissionNotStarted {
		t.Fatalf("submission = %v ok=%v err=%v", state, ok, err)
	}
	if got := pendingRequestCount(connection); got != 0 {
		t.Fatalf("pending requests = %d, want 0", got)
	}
}

func TestPreparedRequestPendingLimitIncludesUndispatched(t *testing.T) {
	readSide, keepOpen := io.Pipe()
	defer func() { _ = keepOpen.Close() }()
	opts := testOptions()
	opts.MaxPendingRequests = 1
	connection, err := NewConnectionWithOptions(nil, io.Discard, readSide, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	first, err := PrepareRequest[json.RawMessage](connection, "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareRequest[json.RawMessage](connection, "second", nil); !errors.Is(err, ErrPendingRequestsExceeded) {
		t.Fatalf("second PrepareRequest error = %v", err)
	}
	first.Abandon()
	second, err := PrepareRequest[json.RawMessage](connection, "second", nil)
	if err != nil {
		t.Fatalf("PrepareRequest after Abandon: %v", err)
	}
	second.Abandon()
}

func TestPreparedRequestCancelRequestKeepsResponsePending(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	original := make(chan anyMessage, 1)
	cancelSeen := make(chan anyMessage, 1)
	go func() {
		reader := bufio.NewReader(peerSide)
		for _, destination := range []chan anyMessage{original, cancelSeen} {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				return
			}
			var message anyMessage
			if json.Unmarshal(line, &message) != nil {
				return
			}
			destination <- message
		}
		request := <-original
		response, encodeErr := encodeMessage(anyMessage{ID: request.ID, Result: json.RawMessage(`"cancel-raced"`)})
		if encodeErr == nil {
			_, _ = peerSide.Write(response)
		}
	}()

	request, err := PrepareRequest[string](connection, "cancelable", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := request.Dispatch(ctx, DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := request.CancelRequest(ctx); err != nil {
		t.Fatal(err)
	}
	if err := request.CancelRequest(ctx); err != nil {
		t.Fatalf("second CancelRequest: %v", err)
	}
	cancelMessage := <-cancelSeen
	if cancelMessage.Method != "$/cancel_request" {
		t.Fatalf("cancel method = %q", cancelMessage.Method)
	}
	result, err := request.Wait(ctx)
	if err != nil || result != "cancel-raced" {
		t.Fatalf("Wait = %q, %v", result, err)
	}
}

func TestPreparedRequestWaitCanRetryAfterTimeout(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	releaseResponse := make(chan struct{})
	go func() {
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var request anyMessage
		if json.Unmarshal(line, &request) != nil {
			return
		}
		<-releaseResponse
		response, encodeErr := encodeMessage(anyMessage{ID: request.ID, Result: json.RawMessage(`"late"`)})
		if encodeErr == nil {
			_, _ = peerSide.Write(response)
		}
	}()
	request, err := PrepareRequest[string](connection, "retry-wait", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Dispatch(context.Background(), DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	shortCtx, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	if _, err := request.Wait(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Wait error = %v", err)
	}
	close(releaseResponse)
	ctx, cancel := waitContext(t)
	defer cancel()
	result, err := request.Wait(ctx)
	if err != nil || result != "late" {
		t.Fatalf("second Wait = %q, %v", result, err)
	}
}

func TestPreparedRequestNotificationWaitPreservesContextCause(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	notificationStarted := make(chan struct{})
	releaseNotification := make(chan struct{})
	connection, err := NewConnectionWithOptions(func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
		if method != "before" {
			return nil, NewMethodNotFound(method)
		}
		close(notificationStarted)
		<-releaseNotification
		return nil, nil
	}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	go func() {
		reader := bufio.NewReader(peerSide)
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			return
		}
		var request anyMessage
		if json.Unmarshal(line, &request) != nil {
			return
		}
		before, _ := encodeMessage(anyMessage{Method: "before"})
		response, _ := encodeMessage(anyMessage{ID: request.ID, Result: json.RawMessage(`"ok"`)})
		_, _ = peerSide.Write(before)
		_, _ = peerSide.Write(response)
	}()

	request, err := PrepareRequest[string](connection, "wait-notifications", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := request.Dispatch(context.Background(), DispatchOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-notificationStarted:
	case <-time.After(testTimeout):
		t.Fatal("notification did not start")
	}
	shortCtx, cancelShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelShort()
	if _, err := request.Wait(shortCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first Wait error = %T %v, want context deadline", err, err)
	}
	close(releaseNotification)
	ctx, cancel := waitContext(t)
	defer cancel()
	result, err := request.Wait(ctx)
	if err != nil || result != "ok" {
		t.Fatalf("second Wait = %q, %v", result, err)
	}
}

func TestSendRequestWithResponseHookDoesNotOverwriteHookErrorAfterCancel(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	go respondToOneRequest(peerSide, func(request anyMessage) anyMessage {
		return anyMessage{ID: request.ID, Result: json.RawMessage(`"ok"`)}
	})

	ctx, cancel := context.WithCancel(context.Background())
	hookErr := errors.New("hook rejected response")
	_, err = SendRequestWithResponseHook[string](connection, ctx, "hook", nil, func(context.Context, string) error {
		cancel()
		return hookErr
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("SendRequestWithResponseHook error = %T %v, want hook error", err, err)
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) && requestErr.Code == -32800 {
		t.Fatalf("hook error was overwritten by cancellation: %v", err)
	}
}

func TestPreparedRequestConcurrentCancelAbandonClose(t *testing.T) {
	for range 50 {
		readSide, keepOpen := io.Pipe()
		connection, err := NewConnectionWithOptions(nil, io.Discard, readSide, testOptions())
		if err != nil {
			t.Fatal(err)
		}
		request, err := PrepareRequest[json.RawMessage](connection, "race", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := request.Dispatch(context.Background(), DispatchOptions{}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			_ = request.CancelRequest(context.Background())
		}()
		go func() {
			defer wg.Done()
			request.Abandon()
		}()
		go func() {
			defer wg.Done()
			_ = connection.Close()
		}()
		wg.Wait()
		_ = keepOpen.Close()
		if got := pendingRequestCount(connection); got != 0 {
			t.Fatalf("pending requests = %d, want 0", got)
		}
	}
}

type lifecycleBlockingWriter struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu    sync.Mutex
	calls int
}

func newLifecycleBlockingWriter() *lifecycleBlockingWriter {
	return &lifecycleBlockingWriter{started: make(chan struct{}), release: make(chan struct{})}
}

func (w *lifecycleBlockingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

func (w *lifecycleBlockingWriter) releaseWrite() {
	select {
	case <-w.release:
	default:
		close(w.release)
	}
}

func (w *lifecycleBlockingWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

type lifecycleFixedWriter struct {
	written int
	err     error
}

type fakeSubmissionError struct{}

func (fakeSubmissionError) Error() string { return "fake submission classification" }

func (fakeSubmissionError) SubmissionState() RequestSubmissionState {
	return RequestSubmissionNotStarted
}

func (w lifecycleFixedWriter) Write([]byte) (int, error) {
	return w.written, w.err
}

func waitForWriteQueueLength(t *testing.T, connection *Connection, want int) {
	t.Helper()
	deadline := time.After(testTimeout)
	for len(connection.writeQueue) != want {
		select {
		case <-deadline:
			t.Fatalf("queued writes = %d, want %d", len(connection.writeQueue), want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func waitForEnqueuedNotificationSeq(t *testing.T, connection *Connection, want uint64) {
	t.Helper()
	deadline := time.After(testTimeout)
	for {
		connection.notifyMu.Lock()
		got := connection.lastEnqueuedNotificationSeq
		connection.notifyMu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("last enqueued notification = %d, want at least %d", got, want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func pendingRequestCount(connection *Connection) int {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	return len(connection.pending)
}

func respondToOneRequest(peer net.Conn, response func(anyMessage) anyMessage) {
	reader := bufio.NewReader(peer)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return
	}
	var request anyMessage
	if json.Unmarshal(line, &request) != nil {
		return
	}
	encoded, err := encodeMessage(response(request))
	if err == nil {
		_, _ = peer.Write(encoded)
	}
}
