package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type preloadedBlockingReader struct {
	reader bytes.Reader
	done   chan struct{}
	once   sync.Once
	closed atomic.Bool
}

func newPreloadedBlockingReader(frame string) *preloadedBlockingReader {
	r := &preloadedBlockingReader{done: make(chan struct{})}
	r.reader.Reset([]byte(frame))
	return r
}

func (r *preloadedBlockingReader) Read(p []byte) (int, error) {
	if r.reader.Len() > 0 {
		return r.reader.Read(p)
	}
	<-r.done
	return 0, io.EOF
}

func (r *preloadedBlockingReader) Close() error {
	r.once.Do(func() {
		r.closed.Store(true)
		close(r.done)
	})
	return nil
}

type captureWriteCloser struct {
	mu     sync.Mutex
	closed bool
	writes chan []byte
}

func newCaptureWriteCloser() *captureWriteCloser {
	return &captureWriteCloser{writes: make(chan []byte, 4)}
}

func (w *captureWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	copyOfP := append([]byte(nil), p...)
	w.writes <- copyOfP
	return len(p), nil
}

func (w *captureWriteCloser) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return nil
}

func (w *captureWriteCloser) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

type readinessAgent struct {
	minimalAgent
	seen chan error
}

func (a *readinessAgent) Prompt(ctx context.Context, _ PromptRequest) (PromptResponse, error) {
	peer, ok := AgentSideConnectionFromContext(ctx)
	if !ok {
		a.seen <- io.ErrUnexpectedEOF
		return PromptResponse{StopReason: StopReasonEndTurn}, nil
	}
	err := peer.SessionUpdateRaw(ctx, json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"future","ready":true}}`))
	a.seen <- err
	return PromptResponse{StopReason: StopReasonEndTurn}, nil
}

func TestAgentSideConnectionIsReadyBeforeFirstCallback(t *testing.T) {
	t.Parallel()
	reader := newPreloadedBlockingReader(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"s1","prompt":[]}}` + "\n")
	writer := newCaptureWriteCloser()
	implementation := &readinessAgent{seen: make(chan error, 1)}
	connection, err := NewAgentSideConnectionWithOptions(implementation, writer, reader, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	select {
	case err := <-implementation.seen:
		if err != nil {
			t.Fatalf("reverse session/update failed: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("first Agent callback deadlocked during reverse call")
	}
	message := receiveCapturedMessage(t, writer.writes)
	if message.ID != nil || message.Method != ClientMethodSessionUpdate {
		t.Fatalf("first reverse message = %#v, want session/update notification", message)
	}
}

type readinessClient struct {
	seen chan readinessClientObservation
}

type readinessClientObservation struct {
	raw json.RawMessage
	err error
}

func (c *readinessClient) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}

func (c *readinessClient) SessionUpdate(ctx context.Context, _ SessionNotification) error {
	peer, ok := ClientSideConnectionFromContext(ctx)
	if !ok {
		c.seen <- readinessClientObservation{err: io.ErrUnexpectedEOF}
		return nil
	}
	raw, ok := InboundParamsFromContext(ctx)
	if !ok {
		c.seen <- readinessClientObservation{err: io.ErrUnexpectedEOF}
		return nil
	}
	err := peer.Cancel(ctx, CancelNotification{SessionId: "s1"})
	c.seen <- readinessClientObservation{raw: raw, err: err}
	return nil
}

func TestClientSideConnectionIsReadyBeforeFirstCallback(t *testing.T) {
	t.Parallel()
	reader := newPreloadedBlockingReader(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ready"},"future":{"keep":true}},"unknownTop":[1,2]}}` + "\n")
	writer := newCaptureWriteCloser()
	implementation := &readinessClient{seen: make(chan readinessClientObservation, 1)}
	connection, err := NewClientSideConnectionWithOptions(implementation, writer, reader, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()

	select {
	case observation := <-implementation.seen:
		if observation.err != nil {
			t.Fatalf("reverse session/cancel failed: %v", observation.err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(observation.raw, &raw); err != nil {
			t.Fatal(err)
		}
		if string(raw["unknownTop"]) != `[1,2]` {
			t.Fatalf("notification raw params lost unknown field: %s", observation.raw)
		}
	case <-time.After(testTimeout):
		t.Fatal("first Client callback deadlocked during reverse call")
	}
	message := receiveCapturedMessage(t, writer.writes)
	if message.ID != nil || message.Method != AgentMethodSessionCancel {
		t.Fatalf("first reverse message = %#v, want session/cancel notification", message)
	}
}

func TestTypedConnectionConstructionFailureDoesNotStartOrOwnStreams(t *testing.T) {
	t.Parallel()
	reader := newPreloadedBlockingReader(`{"jsonrpc":"2.0","id":1,"method":"session/prompt","params":{"sessionId":"s1","prompt":[]}}` + "\n")
	writer := newCaptureWriteCloser()
	implementation := &readinessAgent{seen: make(chan error, 1)}
	opts := testOptions()
	opts.MaxFrameSize = -1

	connection, err := NewAgentSideConnectionWithOptions(implementation, writer, reader, opts)
	if err == nil || connection != nil {
		t.Fatalf("connection, error = %#v, %v; want construction failure", connection, err)
	}
	if reader.closed.Load() || writer.isClosed() {
		t.Fatal("failed construction took ownership of streams")
	}
	select {
	case callbackErr := <-implementation.seen:
		t.Fatalf("failed construction started callback: %v", callbackErr)
	default:
	}
	_ = reader.Close()
	_ = writer.Close()
}

func receiveCapturedMessage(t *testing.T, writes <-chan []byte) anyMessage {
	t.Helper()
	select {
	case frame := <-writes:
		var message anyMessage
		if err := json.Unmarshal(frame, &message); err != nil {
			t.Fatalf("decode captured message: %v", err)
		}
		return message
	case <-time.After(testTimeout):
		t.Fatal("reverse message was not written")
		return anyMessage{}
	}
}

var _ Agent = (*readinessAgent)(nil)
var _ Client = (*readinessClient)(nil)
