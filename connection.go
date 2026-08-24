package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	defaultMaxFrameSize           = 10 * 1024 * 1024
	defaultMaxPendingRequests     = 1024
	defaultMaxHandlerConcurrency  = 32
	defaultMaxQueuedRequests      = 128
	defaultMaxQueuedNotifications = 1024
	defaultMaxQueuedWrites        = 1024
)

var (
	ErrConnectionClosed        = errors.New("acp: connection closed")
	ErrPeerClosed              = errors.New("acp: peer closed connection")
	ErrFrameTooLarge           = errors.New("acp: frame exceeds configured limit")
	ErrPendingRequestsExceeded = errors.New("acp: pending request limit exceeded")
	ErrRequestQueueFull        = errors.New("acp: inbound request queue full")
	ErrNotificationQueueFull   = errors.New("acp: inbound notification queue full")
)

// ConnectionOptions bounds every connection-owned queue and source of
// concurrency. Zero values select the documented defaults.
type ConnectionOptions struct {
	MaxFrameSize           int
	MaxPendingRequests     int
	MaxHandlerConcurrency  int
	MaxQueuedRequests      int
	MaxQueuedNotifications int
	MaxQueuedWrites        int
	Logger                 *slog.Logger
}

// DefaultConnectionOptions returns the production defaults used by
// NewConnection.
func DefaultConnectionOptions() ConnectionOptions {
	return ConnectionOptions{
		MaxFrameSize:           defaultMaxFrameSize,
		MaxPendingRequests:     defaultMaxPendingRequests,
		MaxHandlerConcurrency:  defaultMaxHandlerConcurrency,
		MaxQueuedRequests:      defaultMaxQueuedRequests,
		MaxQueuedNotifications: defaultMaxQueuedNotifications,
		MaxQueuedWrites:        defaultMaxQueuedWrites,
	}
}

func normalizeConnectionOptions(opts ConnectionOptions) (ConnectionOptions, error) {
	defaults := DefaultConnectionOptions()
	if opts.MaxFrameSize == 0 {
		opts.MaxFrameSize = defaults.MaxFrameSize
	}
	if opts.MaxPendingRequests == 0 {
		opts.MaxPendingRequests = defaults.MaxPendingRequests
	}
	if opts.MaxHandlerConcurrency == 0 {
		opts.MaxHandlerConcurrency = defaults.MaxHandlerConcurrency
	}
	if opts.MaxQueuedRequests == 0 {
		opts.MaxQueuedRequests = defaults.MaxQueuedRequests
	}
	if opts.MaxQueuedNotifications == 0 {
		opts.MaxQueuedNotifications = defaults.MaxQueuedNotifications
	}
	if opts.MaxQueuedWrites == 0 {
		opts.MaxQueuedWrites = defaults.MaxQueuedWrites
	}
	if opts.MaxFrameSize < 1 ||
		opts.MaxPendingRequests < 1 ||
		opts.MaxHandlerConcurrency < 1 ||
		opts.MaxQueuedRequests < 1 ||
		opts.MaxQueuedNotifications < 1 ||
		opts.MaxQueuedWrites < 1 {
		return ConnectionOptions{}, errors.New("acp: connection limits must be positive")
	}
	return opts, nil
}

type anyMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RequestError    `json:"error,omitempty"`
}

// UnmarshalJSON preserves the distinction between an omitted id and the
// explicit JSON-RPC id value null. encoding/json otherwise decodes both into a
// nil *json.RawMessage, which would misclassify null-id requests as
// notifications.
func (m *anyMessage) UnmarshalJSON(data []byte) error {
	type wireMessage anyMessage
	var decoded wireMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if rawID, ok := fields["id"]; ok {
		id := json.RawMessage(append([]byte(nil), rawID...))
		decoded.ID = &id
	}
	*m = anyMessage(decoded)
	return nil
}

type responseEnvelope struct {
	msg                   anyMessage
	notificationWatermark uint64
}

type pendingResponse struct {
	ch chan responseEnvelope
}

type cancelRequestParams struct {
	RequestID json.RawMessage `json:"requestId"`
}

type queuedNotification struct {
	seq uint64
	msg anyMessage
}

type notificationSequenceContextKey struct{}

type notificationFrame struct {
	mu     sync.Mutex
	seq    uint64
	active bool
}

type queuedRequest struct {
	msg    anyMessage
	idKey  string
	ctx    context.Context
	cancel context.CancelCauseFunc
}

type queuedWrite struct {
	ctx  context.Context
	data []byte
	done chan error
}

// MethodHandler dispatches one inbound ACP request or notification.
type MethodHandler func(ctx context.Context, method string, params json.RawMessage) (any, *RequestError)

// Connection is a bounded, bidirectional JSON-RPC 2.0 connection over
// newline-delimited JSON.
type Connection struct {
	w       io.Writer
	r       io.Reader
	handler MethodHandler
	opts    ConnectionOptions

	ctx    context.Context
	cancel context.CancelCauseFunc

	nextID atomic.Uint64
	logger atomic.Pointer[slog.Logger]

	mu       sync.Mutex
	pending  map[string]*pendingResponse
	inflight map[string]context.CancelCauseFunc

	notifyMu                    sync.Mutex
	lastEnqueuedNotificationSeq uint64
	completedNotificationSeq    uint64
	completedNotifications      map[uint64]struct{}
	notificationProgress        chan struct{}

	requestQueue      chan queuedRequest
	notificationQueue chan queuedNotification
	writeQueue        chan queuedWrite
	cancelQueue       chan string

	shutdownOnce sync.Once
	wg           sync.WaitGroup
	waitDone     chan struct{}
}

// NewConnection creates a connection with bounded production defaults.
func NewConnection(handler MethodHandler, peerInput io.Writer, peerOutput io.Reader) *Connection {
	c, err := NewConnectionWithOptions(handler, peerInput, peerOutput, ConnectionOptions{})
	if err != nil {
		panic(err)
	}
	return c
}

// NewConnectionWithOptions creates a connection with explicit resource
// limits. The connection owns peerInput and peerOutput and closes them when
// the connection shuts down if they implement io.Closer.
func NewConnectionWithOptions(handler MethodHandler, peerInput io.Writer, peerOutput io.Reader, opts ConnectionOptions) (*Connection, error) {
	if peerInput == nil || peerOutput == nil {
		return nil, errors.New("acp: peer input and output are required")
	}
	normalized, err := normalizeConnectionOptions(opts)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	c := &Connection{
		w:                      peerInput,
		r:                      peerOutput,
		handler:                handler,
		opts:                   normalized,
		ctx:                    ctx,
		cancel:                 cancel,
		pending:                make(map[string]*pendingResponse),
		inflight:               make(map[string]context.CancelCauseFunc),
		completedNotifications: make(map[uint64]struct{}),
		notificationProgress:   make(chan struct{}, 1),
		requestQueue:           make(chan queuedRequest, normalized.MaxQueuedRequests),
		notificationQueue:      make(chan queuedNotification, normalized.MaxQueuedNotifications),
		writeQueue:             make(chan queuedWrite, normalized.MaxQueuedWrites),
		cancelQueue:            make(chan string, normalized.MaxPendingRequests),
		waitDone:               make(chan struct{}),
	}
	if normalized.Logger != nil {
		c.logger.Store(normalized.Logger)
	}

	workerCount := normalized.MaxHandlerConcurrency
	c.wg.Add(4 + workerCount)
	go c.receive()
	go c.processNotifications()
	go c.processWrites()
	go c.processCancelRequests()
	for range workerCount {
		go c.processRequests()
	}
	go func() {
		c.wg.Wait()
		close(c.waitDone)
	}()
	return c, nil
}

// SetLogger installs a logger used for non-payload diagnostics.
func (c *Connection) SetLogger(logger *slog.Logger) {
	c.logger.Store(logger)
}

func (c *Connection) loggerOrDefault() *slog.Logger {
	if logger := c.logger.Load(); logger != nil {
		return logger
	}
	return slog.Default()
}

func (c *Connection) receive() {
	defer c.wg.Done()

	initialSize := 64 * 1024
	if c.opts.MaxFrameSize < initialSize {
		initialSize = c.opts.MaxFrameSize
	}
	scanner := bufio.NewScanner(c.r)
	scanner.Buffer(make([]byte, 0, initialSize), c.opts.MaxFrameSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var msg anyMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.loggerOrDefault().Warn("discarding malformed JSON-RPC frame", "err", err)
			c.sendProtocolError(NewParseError(nil))
			continue
		}
		if msg.JSONRPC != "2.0" {
			c.loggerOrDefault().Warn("discarding frame with invalid jsonrpc version")
			c.sendProtocolError(NewInvalidRequest(nil))
			continue
		}

		if msg.ID == nil && msg.Method == "$/cancel_request" {
			c.handleCancelRequest(&msg)
			continue
		}

		switch {
		case msg.ID != nil && msg.Method == "":
			c.handleResponse(&msg)
		case msg.Method != "" && msg.ID != nil:
			c.enqueueRequest(msg)
		case msg.Method != "":
			if !c.enqueueNotification(msg) {
				c.shutdown(ErrNotificationQueueFull)
				return
			}
		default:
			c.loggerOrDefault().Warn("discarding JSON-RPC frame without id or method")
			c.sendProtocolError(NewInvalidRequest(nil))
		}
	}

	cause := fmt.Errorf("%w: EOF", ErrPeerClosed)
	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "token too long") {
			cause = fmt.Errorf("%w: %v", ErrFrameTooLarge, err)
		} else {
			cause = fmt.Errorf("%w: %v", ErrPeerClosed, err)
		}
	}
	c.shutdown(cause)
}

func (c *Connection) sendProtocolError(reqErr *RequestError) {
	nullID := json.RawMessage("null")
	_ = c.sendMessage(c.ctx, anyMessage{ID: &nullID, Error: reqErr})
}

func (c *Connection) enqueueRequest(msg anyMessage) {
	idKey, err := canonicalJSONRPCIDKey(*msg.ID)
	if err != nil {
		_ = c.sendMessage(c.ctx, anyMessage{ID: msg.ID, Error: NewInvalidRequest(map[string]any{"error": "invalid request id"})})
		return
	}

	reqCtx, cancel := context.WithCancelCause(c.ctx)
	c.mu.Lock()
	if _, duplicate := c.inflight[idKey]; duplicate {
		c.mu.Unlock()
		cancel(nil)
		_ = c.sendMessage(c.ctx, anyMessage{ID: msg.ID, Error: NewInvalidRequest(map[string]any{"error": "duplicate request id"})})
		return
	}
	c.inflight[idKey] = cancel
	c.mu.Unlock()

	req := queuedRequest{msg: msg, idKey: idKey, ctx: reqCtx, cancel: cancel}
	select {
	case c.requestQueue <- req:
	case <-c.Done():
		c.removeInflight(idKey)
		cancel(context.Cause(c.ctx))
	default:
		c.removeInflight(idKey)
		cancel(ErrRequestQueueFull)
		_ = c.sendMessage(c.ctx, anyMessage{ID: msg.ID, Error: NewServerOverloaded(map[string]any{"error": ErrRequestQueueFull.Error()})})
	}
}

func (c *Connection) enqueueNotification(msg anyMessage) bool {
	c.notifyMu.Lock()
	seq := c.lastEnqueuedNotificationSeq + 1
	queued := queuedNotification{seq: seq, msg: msg}
	select {
	case c.notificationQueue <- queued:
		c.lastEnqueuedNotificationSeq = seq
		c.notifyMu.Unlock()
		return true
	default:
		c.notifyMu.Unlock()
		return false
	}
}

func (c *Connection) processRequests() {
	defer c.wg.Done()
	for {
		select {
		case <-c.Done():
			return
		case req := <-c.requestQueue:
			c.handleInbound(req.ctx, &req.msg)
			c.removeInflight(req.idKey)
			req.cancel(nil)
		}
	}
}

func (c *Connection) processNotifications() {
	defer c.wg.Done()
	for {
		select {
		case <-c.Done():
			return
		case queued := <-c.notificationQueue:
			c.processNotification(queued)
		}
	}
}

func (c *Connection) processNotification(queued queuedNotification) {
	frame := &notificationFrame{seq: queued.seq, active: true}
	handlerCtx, cancel := context.WithCancel(context.WithValue(c.ctx, notificationSequenceContextKey{}, frame))
	c.handleInbound(handlerCtx, &queued.msg)
	frame.mu.Lock()
	frame.active = false
	frame.mu.Unlock()
	cancel()
	c.markNotificationComplete(queued.seq)
}

func (c *Connection) markNotificationComplete(seq uint64) {
	c.notifyMu.Lock()
	c.completedNotifications[seq] = struct{}{}
	for {
		next := c.completedNotificationSeq + 1
		if _, ok := c.completedNotifications[next]; !ok {
			break
		}
		delete(c.completedNotifications, next)
		c.completedNotificationSeq = next
	}
	c.notifyMu.Unlock()
	select {
	case c.notificationProgress <- struct{}{}:
	default:
	}
}

func (c *Connection) notificationAlreadyProcessed(seq uint64) bool {
	c.notifyMu.Lock()
	defer c.notifyMu.Unlock()
	if c.completedNotificationSeq >= seq {
		return true
	}
	_, ok := c.completedNotifications[seq]
	return ok
}

func (c *Connection) processWrites() {
	defer c.wg.Done()
	for {
		select {
		case <-c.Done():
			return
		default:
		}
		select {
		case <-c.Done():
			return
		case write := <-c.writeQueue:
			if err := write.ctx.Err(); err != nil {
				write.done <- err
				continue
			}
			n, err := c.w.Write(write.data)
			if err == nil && n != len(write.data) {
				err = io.ErrShortWrite
			}
			write.done <- err
			if err != nil {
				c.shutdown(fmt.Errorf("acp: write failed: %w", err))
				return
			}
		}
	}
}

func (c *Connection) processCancelRequests() {
	defer c.wg.Done()
	for {
		select {
		case <-c.Done():
			return
		case idKey := <-c.cancelQueue:
			requestID := json.RawMessage(append([]byte(nil), idKey...))
			if err := c.SendNotification(c.ctx, "$/cancel_request", cancelRequestParams{RequestID: requestID}); err != nil && c.ctx.Err() == nil {
				c.loggerOrDefault().Debug("failed to send cancellation notification", "err", err)
			}
		}
	}
}

func (c *Connection) handleResponse(msg *anyMessage) {
	idKey, err := canonicalJSONRPCIDKey(*msg.ID)
	if err != nil {
		c.loggerOrDefault().Warn("discarding response with invalid id", "err", err)
		return
	}

	c.mu.Lock()
	pending := c.pending[idKey]
	if pending != nil {
		delete(c.pending, idKey)
	}
	c.mu.Unlock()
	if pending == nil {
		return
	}

	c.notifyMu.Lock()
	watermark := c.lastEnqueuedNotificationSeq
	c.notifyMu.Unlock()
	pending.ch <- responseEnvelope{msg: *msg, notificationWatermark: watermark}
}

func (c *Connection) handleCancelRequest(msg *anyMessage) {
	var params cancelRequestParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		c.loggerOrDefault().Warn("discarding invalid cancellation notification", "err", err)
		return
	}
	idKey, err := canonicalJSONRPCIDKey(params.RequestID)
	if err != nil {
		c.loggerOrDefault().Warn("discarding cancellation with invalid request id", "err", err)
		return
	}

	c.mu.Lock()
	cancel := c.inflight[idKey]
	c.mu.Unlock()
	if cancel != nil {
		cancel(context.Canceled)
	}
}

func (c *Connection) handleInbound(ctx context.Context, req *anyMessage) {
	response := anyMessage{ID: req.ID}
	if c.handler == nil {
		if req.ID != nil {
			response.Error = NewMethodNotFound(req.Method)
			_ = c.sendMessage(c.ctx, response)
		}
		return
	}

	result, reqErr := c.handler(ctx, req.Method, req.Params)
	if req.ID == nil {
		if reqErr != nil && (reqErr.Code != -32601 || !strings.HasPrefix(req.Method, "_")) {
			c.loggerOrDefault().Warn("notification handler failed", "method", req.Method, "err", reqErr)
		}
		return
	}

	if reqErr != nil {
		response.Error = reqErr
	} else {
		encoded, err := json.Marshal(result)
		if err != nil {
			response.Error = NewInternalError(map[string]any{"error": err.Error()})
		} else {
			response.Result = encoded
		}
	}
	_ = c.sendMessage(c.ctx, response)
}

func (c *Connection) sendMessage(ctx context.Context, msg anyMessage) error {
	msg.JSONRPC = "2.0"
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')

	write := queuedWrite{ctx: ctx, data: encoded, done: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-c.Done():
		return c.connectionCause()
	case c.writeQueue <- write:
	}

	select {
	case err := <-write.done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-c.Done():
		return c.connectionCause()
	}
}

// SendRequest sends a JSON-RPC request and decodes its typed result.
func SendRequest[T any](c *Connection, ctx context.Context, method string, params any) (T, error) {
	var result T
	msg, idKey, err := c.prepareRequest(method, params)
	if err != nil {
		return result, err
	}
	pending, err := c.addPending(idKey)
	if err != nil {
		return result, err
	}
	if err := c.sendMessage(ctx, msg); err != nil {
		c.cleanupPending(idKey)
		if ctx.Err() != nil {
			return result, toReqErr(context.Cause(ctx))
		}
		return result, err
	}

	response, err := c.waitForResponse(ctx, pending, idKey)
	if err != nil {
		return result, err
	}
	if err := c.waitNotificationsUpTo(ctx, response.notificationWatermark); err != nil {
		return result, err
	}
	if response.msg.Error != nil {
		return result, response.msg.Error
	}
	if len(response.msg.Result) != 0 {
		if err := json.Unmarshal(response.msg.Result, &result); err != nil {
			return result, NewInternalError(map[string]any{"error": err.Error()})
		}
	}
	return result, nil
}

func (c *Connection) prepareRequest(method string, params any) (anyMessage, string, error) {
	if method == "" {
		return anyMessage{}, "", NewInvalidRequest(map[string]any{"error": "method is required"})
	}
	id := c.nextID.Add(1)
	idRaw, err := json.Marshal(id)
	if err != nil {
		return anyMessage{}, "", err
	}
	msg := anyMessage{ID: (*json.RawMessage)(&idRaw), Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return anyMessage{}, "", NewInvalidParams(map[string]any{"error": err.Error()})
		}
		msg.Params = encoded
	}
	return msg, string(idRaw), nil
}

func (c *Connection) addPending(idKey string) (*pendingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) >= c.opts.MaxPendingRequests {
		return nil, ErrPendingRequestsExceeded
	}
	pending := &pendingResponse{ch: make(chan responseEnvelope, 1)}
	c.pending[idKey] = pending
	return pending, nil
}

func (c *Connection) waitForResponse(ctx context.Context, pending *pendingResponse, idKey string) (responseEnvelope, error) {
	select {
	case response := <-pending.ch:
		return response, nil
	case <-ctx.Done():
		c.cleanupPending(idKey)
		c.queueCancel(idKey)
		return responseEnvelope{}, toReqErr(context.Cause(ctx))
	case <-c.Done():
		c.cleanupPending(idKey)
		return responseEnvelope{}, c.connectionCause()
	}
}

func (c *Connection) waitNotificationsUpTo(ctx context.Context, target uint64) error {
	// A notification handler may issue a request back to the peer. If the peer
	// sends more notifications before responding, process those notifications
	// in the current ordered call stack. This avoids waiting for the current
	// notification to complete itself while retaining the response barrier for
	// progress sent before the response.
	if frame, ok := ctx.Value(notificationSequenceContextKey{}).(*notificationFrame); ok && target >= frame.seq {
		if handled, err := c.waitNotificationsReentrant(ctx, target, frame); handled {
			return err
		}
	}
	for {
		c.notifyMu.Lock()
		complete := c.completedNotificationSeq >= target
		c.notifyMu.Unlock()
		if complete {
			return nil
		}
		select {
		case <-ctx.Done():
			return toReqErr(context.Cause(ctx))
		case <-c.Done():
			return c.connectionCause()
		case <-c.notificationProgress:
		}
	}
}

func (c *Connection) waitNotificationsReentrant(ctx context.Context, target uint64, frame *notificationFrame) (bool, error) {
	frame.mu.Lock()
	defer frame.mu.Unlock()
	if !frame.active {
		return false, nil
	}
	for next := frame.seq + 1; next <= target; next++ {
		if c.notificationAlreadyProcessed(next) {
			continue
		}
		select {
		case <-ctx.Done():
			return true, toReqErr(context.Cause(ctx))
		case <-c.Done():
			return true, c.connectionCause()
		case queued := <-c.notificationQueue:
			if queued.seq != next {
				c.shutdown(errors.New("acp: notification sequence invariant violated"))
				return true, c.connectionCause()
			}
			c.processNotification(queued)
		}
	}
	return true, nil
}

func (c *Connection) queueCancel(idKey string) {
	select {
	case <-c.Done():
	case c.cancelQueue <- idKey:
	default:
		c.loggerOrDefault().Debug("dropping cancellation because queue is full")
	}
}

func (c *Connection) cleanupPending(idKey string) {
	c.mu.Lock()
	delete(c.pending, idKey)
	c.mu.Unlock()
}

func (c *Connection) removeInflight(idKey string) {
	c.mu.Lock()
	delete(c.inflight, idKey)
	c.mu.Unlock()
}

// SendRequestNoResult sends a request whose successful response has no result.
func (c *Connection) SendRequestNoResult(ctx context.Context, method string, params any) error {
	_, err := SendRequest[json.RawMessage](c, ctx, method, params)
	return err
}

// SendNotification sends a JSON-RPC notification.
func (c *Connection) SendNotification(ctx context.Context, method string, params any) error {
	if method == "" {
		return NewInvalidRequest(map[string]any{"error": "method is required"})
	}
	msg := anyMessage{Method: method}
	if params != nil {
		encoded, err := json.Marshal(params)
		if err != nil {
			return NewInvalidParams(map[string]any{"error": err.Error()})
		}
		msg.Params = encoded
	}
	return c.sendMessage(ctx, msg)
}

func (c *Connection) shutdown(cause error) {
	if cause == nil {
		cause = ErrConnectionClosed
	}
	c.shutdownOnce.Do(func() {
		c.cancel(cause)
		if closer, ok := c.r.(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := c.w.(io.Closer); ok {
			_ = closer.Close()
		}
		select {
		case c.notificationProgress <- struct{}{}:
		default:
		}
	})
}

func (c *Connection) connectionCause() error {
	if cause := context.Cause(c.ctx); cause != nil {
		return cause
	}
	return ErrConnectionClosed
}

// Close idempotently terminates the connection and closes owned streams.
func (c *Connection) Close() error {
	c.shutdown(ErrConnectionClosed)
	return nil
}

// Wait blocks until all connection-owned goroutines terminate.
func (c *Connection) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-c.waitDone:
		return c.connectionCause()
	}
}

// Done is closed when the connection begins shutting down.
func (c *Connection) Done() <-chan struct{} {
	return c.ctx.Done()
}

// Err reports the immutable connection shutdown cause, or nil while active.
func (c *Connection) Err() error {
	return context.Cause(c.ctx)
}
