package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrPreparedRequestUnavailable  = errors.New("acp: prepared request is unavailable")
	ErrRequestAlreadyDispatched    = errors.New("acp: prepared request was already dispatched")
	ErrRequestNotDispatched        = errors.New("acp: prepared request was not dispatched")
	ErrRequestAbandoned            = errors.New("acp: prepared request was abandoned")
	ErrResponseObserverRequired    = errors.New("acp: response observer is required")
	ErrResponseObserverRegistered  = errors.New("acp: response observer is already registered")
	ErrResponseObserverUnavailable = errors.New("acp: response observer must be registered before dispatch")
)

var errPreparedWriteNotStarted = errors.New("acp: prepared request write was not started")

// RequestSubmissionState is the strongest transport-level statement the SDK
// can make about an outbound request. It does not describe application commit
// or prove that the peer executed the request.
type RequestSubmissionState uint8

const (
	// RequestSubmissionNotStarted proves that the transport writer was never
	// invoked and can no longer be invoked for this request.
	RequestSubmissionNotStarted RequestSubmissionState = iota

	// RequestSubmissionPossible means the transport writer was invoked. This
	// remains true for zero-byte, partial, failed, or blocked writes.
	RequestSubmissionPossible

	// RequestSubmissionPending means the writer has not been observed starting,
	// but the prepared request is still live or racing with Dispatch. It is not
	// safe to infer that the request cannot subsequently be submitted.
	RequestSubmissionPending
)

func (s RequestSubmissionState) String() string {
	switch s {
	case RequestSubmissionNotStarted:
		return "not_submitted"
	case RequestSubmissionPossible:
		return "may_have_been_submitted"
	case RequestSubmissionPending:
		return "submission_pending"
	default:
		return fmt.Sprintf("RequestSubmissionState(%d)", s)
	}
}

// RequestLifecycleError preserves the operation, transport submission state,
// and underlying error for a prepared request operation.
type RequestLifecycleError struct {
	operation  string
	submission RequestSubmissionState
	err        error
	classified bool
}

func (e *RequestLifecycleError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.err == nil {
		return fmt.Sprintf("acp: prepared request %s failed", e.operation)
	}
	return fmt.Sprintf("acp: prepared request %s failed: %v", e.operation, e.err)
}

func (e *RequestLifecycleError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// Operation returns the lifecycle operation that failed.
func (e *RequestLifecycleError) Operation() string {
	if e == nil {
		return ""
	}
	return e.operation
}

// SubmissionState returns the immutable submission classification associated
// with the failure.
func (e *RequestLifecycleError) SubmissionState() RequestSubmissionState {
	if e == nil || !e.classified {
		return RequestSubmissionPending
	}
	return e.submission
}

// RequestSubmissionStateOf extracts an SDK submission classification through
// ordinary error wrapping. ok is false for errors not produced by the prepared
// request lifecycle; callers must not treat an unclassified error as safe to
// retry.
func RequestSubmissionStateOf(err error) (state RequestSubmissionState, ok bool) {
	var lifecycleErr *RequestLifecycleError
	if !errors.As(err, &lifecycleErr) || !lifecycleErr.classified {
		return RequestSubmissionNotStarted, false
	}
	return lifecycleErr.SubmissionState(), true
}

// RequestMayHaveBeenSubmitted conservatively reports whether err can be
// treated as a proven pre-write failure. Unknown errors return true.
func RequestMayHaveBeenSubmitted(err error) bool {
	if err == nil {
		return false
	}
	state, ok := RequestSubmissionStateOf(err)
	return !ok || state != RequestSubmissionNotStarted
}

// ResponseDecodeError reports that a successful JSON-RPC response arrived but
// its result could not be decoded into the requested Go type. The request must
// not be automatically retried.
type ResponseDecodeError struct {
	Err error
}

func (e *ResponseDecodeError) Error() string {
	if e == nil || e.Err == nil {
		return "acp: decode successful response result"
	}
	return fmt.Sprintf("acp: decode successful response result: %v", e.Err)
}

func (e *ResponseDecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RPCResponse is a decode-independent JSON-RPC response envelope. Result and
// RequestID preserve their JSON representation; Error exposes code, message,
// and data through the existing RequestError type.
type RPCResponse struct {
	RequestID json.RawMessage
	Result    json.RawMessage
	Error     *RequestError
}

// ResponseObserver runs once after all earlier notifications complete and
// before typed decoding or Wait completion. It must not wait for work that
// depends on a later notification from the same connection.
type ResponseObserver func(context.Context, RPCResponse) error

// DispatchOptions controls transport revocation for a prepared dispatch.
type DispatchOptions struct {
	// Abort is called at most once when dispatch fails or its context is
	// cancelled after the writer has been invoked. It must revoke the exact
	// transport used by this request so a blocked Write can return. Abort is
	// never called for a proven pre-write failure.
	Abort func(cause error) error
}

type preparedRequestPhase uint8

const (
	preparedRequestReady preparedRequestPhase = iota
	preparedRequestDispatching
	preparedRequestDispatched
	preparedRequestDispatchFailed
	preparedRequestAbandoned
	preparedRequestFinished
)

// PreparedRequest owns one encoded, bounded pending request. Callers must
// eventually call Wait or Abandon. Dispatch and Wait accept independent
// contexts so response ownership can move to a longer-lived producer.
type PreparedRequest[T any] struct {
	conn    *Connection
	idKey   string
	pending *pendingResponse
	payload []byte

	mu             sync.Mutex
	phase          preparedRequestPhase
	submission     RequestSubmissionState
	observer       ResponseObserver
	typedHook      func(context.Context, T) error
	dispatchDone   chan struct{}
	dispatchOnce   sync.Once
	dispatchErr    error
	abandoned      chan struct{}
	abandonOnce    sync.Once
	resolving      bool
	resolveAttempt chan struct{}
	resolved       bool
	result         T
	resultErr      error
	pendingRelease sync.Once

	abortOnce  sync.Once
	abortErr   error
	cancelOnce sync.Once
	cancelErr  error
}

// PrepareRequest encodes and registers a bounded pending request without
// entering the writer queue or touching the transport.
func PrepareRequest[T any](c *Connection, method string, params any) (*PreparedRequest[T], error) {
	return prepareRequestLifecycle[T](c, method, params, true, nil)
}

// PrepareClientRequest prepares a request on a typed client-side connection.
func PrepareClientRequest[T any](c *ClientSideConnection, method string, params any) (*PreparedRequest[T], error) {
	if c == nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	return PrepareRequest[T](c.conn, method, params)
}

// PrepareAgentRequest prepares a reverse request on a typed agent-side
// connection.
func PrepareAgentRequest[T any](c *AgentSideConnection, method string, params any) (*PreparedRequest[T], error) {
	if c == nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	return PrepareRequest[T](c.conn, method, params)
}

func prepareRequestLifecycle[T any](
	c *Connection,
	method string,
	params any,
	gated bool,
	typedHook func(context.Context, T) error,
) (*PreparedRequest[T], error) {
	if c == nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	msg, idKey, err := c.prepareRequest(method, params)
	if err != nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, err)
	}
	payload, err := encodeMessage(msg)
	if err != nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, err)
	}
	pending, err := c.addPending(idKey, gated)
	if err != nil {
		return nil, lifecycleError("prepare", RequestSubmissionNotStarted, err)
	}
	return &PreparedRequest[T]{
		conn:         c,
		idKey:        idKey,
		pending:      pending,
		payload:      payload,
		typedHook:    typedHook,
		dispatchDone: make(chan struct{}),
		abandoned:    make(chan struct{}),
	}, nil
}

// ObserveResponse installs the request's sole decode-independent response
// observer. It must be called before Dispatch.
func (r *PreparedRequest[T]) ObserveResponse(observer ResponseObserver) error {
	if r == nil || r.conn == nil || r.pending == nil {
		return ErrPreparedRequestUnavailable
	}
	if observer == nil {
		return ErrResponseObserverRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase != preparedRequestReady {
		return ErrResponseObserverUnavailable
	}
	if r.observer != nil {
		return ErrResponseObserverRegistered
	}
	r.observer = observer
	return nil
}

// Dispatch writes the prepared request once. A nil return proves that the full
// frame was accepted by the local writer, not that the peer executed it.
func (r *PreparedRequest[T]) Dispatch(ctx context.Context, opts DispatchOptions) error {
	if r == nil || r.conn == nil || r.pending == nil {
		return lifecycleError("dispatch", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	ctx = contextOrBackground(ctx)

	r.mu.Lock()
	if r.phase != preparedRequestReady {
		err := ErrRequestAlreadyDispatched
		if r.phase == preparedRequestAbandoned {
			err = ErrRequestAbandoned
		}
		state := r.submissionStateLocked()
		r.mu.Unlock()
		return lifecycleError("dispatch", state, err)
	}
	r.phase = preparedRequestDispatching
	r.mu.Unlock()

	write := queuedWrite{
		ctx:  ctx,
		data: r.payload,
		done: make(chan writeResult, 1),
		beginWrite: func() error {
			return r.beginWrite(ctx)
		},
	}
	select {
	case <-ctx.Done():
		return r.failDispatch(context.Cause(ctx), opts)
	case <-r.conn.Done():
		return r.failDispatch(r.conn.connectionCause(), opts)
	case <-r.abandoned:
		return r.completeAbandonedDispatch()
	case r.conn.writeQueue <- write:
	}

	for {
		select {
		case result := <-write.done:
			return r.completeWrite(result, opts)
		case <-ctx.Done():
			select {
			case result := <-write.done:
				return r.completeWrite(result, opts)
			default:
			}
			return r.failDispatch(context.Cause(ctx), opts)
		case <-r.conn.Done():
			select {
			case result := <-write.done:
				return r.completeWrite(result, opts)
			default:
			}
			return r.failDispatch(r.conn.connectionCause(), opts)
		case <-r.abandoned:
			return r.completeAbandonedDispatch()
		}
	}
}

func (r *PreparedRequest[T]) beginWrite(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.phase != preparedRequestDispatching {
		if r.phase == preparedRequestAbandoned {
			return ErrRequestAbandoned
		}
		return errPreparedWriteNotStarted
	}
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := r.conn.admitPending(r.idKey, r.pending); err != nil {
		return err
	}
	r.submission = RequestSubmissionPossible
	return nil
}

func (r *PreparedRequest[T]) completeWrite(result writeResult, opts DispatchOptions) error {
	r.mu.Lock()
	state := r.submission
	abandoned := r.phase == preparedRequestAbandoned
	r.mu.Unlock()

	if abandoned {
		return r.completeDispatch(lifecycleError("dispatch", state, ErrRequestAbandoned), preparedRequestAbandoned)
	}
	if result.err != nil {
		cause := result.err
		if state == RequestSubmissionPossible {
			cause = joinAbortError(cause, r.abort(opts, cause))
		}
		r.releasePending(cause)
		return r.completeDispatch(lifecycleError("dispatch", state, cause), preparedRequestDispatchFailed)
	}
	return r.completeDispatch(nil, preparedRequestDispatched)
}

func (r *PreparedRequest[T]) failDispatch(cause error, opts DispatchOptions) error {
	r.mu.Lock()
	state := r.submission
	if r.phase == preparedRequestAbandoned {
		r.mu.Unlock()
		return r.completeAbandonedDispatch()
	}
	if state == RequestSubmissionNotStarted {
		r.phase = preparedRequestDispatchFailed
	}
	r.mu.Unlock()

	if state == RequestSubmissionPossible {
		cause = joinAbortError(cause, r.abort(opts, cause))
	}
	r.releasePending(cause)
	return r.completeDispatch(lifecycleError("dispatch", state, cause), preparedRequestDispatchFailed)
}

func joinAbortError(cause, abortErr error) error {
	if abortErr == nil {
		return cause
	}
	return errors.Join(cause, fmt.Errorf("acp: abort prepared request transport: %w", abortErr))
}

func (r *PreparedRequest[T]) abort(opts DispatchOptions, cause error) error {
	if opts.Abort == nil {
		return nil
	}
	r.abortOnce.Do(func() {
		r.abortErr = opts.Abort(cause)
	})
	return r.abortErr
}

func (r *PreparedRequest[T]) completeAbandonedDispatch() error {
	r.mu.Lock()
	state := r.submissionStateLocked()
	r.mu.Unlock()
	return r.completeDispatch(lifecycleError("dispatch", state, ErrRequestAbandoned), preparedRequestAbandoned)
}

func (r *PreparedRequest[T]) completeDispatch(err error, phase preparedRequestPhase) error {
	r.dispatchOnce.Do(func() {
		r.mu.Lock()
		if r.phase != preparedRequestAbandoned || phase == preparedRequestAbandoned {
			r.phase = phase
		}
		r.dispatchErr = err
		close(r.dispatchDone)
		r.mu.Unlock()
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dispatchErr
}

// CancelRequest sends one best-effort ACP $/cancel_request notification after
// a successful Dispatch. It does not abandon the local waiter or abort the
// transport, and the peer must still complete the original request.
func (r *PreparedRequest[T]) CancelRequest(ctx context.Context) error {
	if r == nil || r.conn == nil || r.pending == nil {
		return lifecycleError("cancel", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	r.mu.Lock()
	phase := r.phase
	state := r.submissionStateLocked()
	r.mu.Unlock()
	if phase != preparedRequestDispatched && phase != preparedRequestFinished {
		return lifecycleError("cancel", state, ErrRequestNotDispatched)
	}
	ctx = contextOrBackground(ctx)
	r.cancelOnce.Do(func() {
		requestID := json.RawMessage(append([]byte(nil), r.idKey...))
		r.cancelErr = r.conn.SendNotification(ctx, "$/cancel_request", cancelRequestParams{RequestID: requestID})
	})
	if r.cancelErr != nil {
		return lifecycleError("cancel", RequestSubmissionPossible, r.cancelErr)
	}
	return nil
}

// Wait waits for the response with a context independent of Dispatch. A Wait
// that ends before a response arrives does not cancel or abandon the request;
// the caller may Wait again or explicitly call CancelRequest or Abandon.
func (r *PreparedRequest[T]) Wait(ctx context.Context) (T, error) {
	var zero T
	if r == nil || r.conn == nil || r.pending == nil {
		return zero, lifecycleError("wait", RequestSubmissionNotStarted, ErrPreparedRequestUnavailable)
	}
	ctx = contextOrBackground(ctx)

	for {
		r.mu.Lock()
		phase := r.phase
		state := r.submissionStateLocked()
		dispatchDone := r.dispatchDone
		dispatchErr := r.dispatchErr
		r.mu.Unlock()
		switch phase {
		case preparedRequestReady:
			select {
			case <-r.pending.done:
				_, err := r.pending.result()
				if err != nil {
					r.mu.Lock()
					if r.phase == preparedRequestReady {
						r.phase = preparedRequestFinished
					}
					r.mu.Unlock()
					return zero, lifecycleError("wait", RequestSubmissionNotStarted, err)
				}
			default:
			}
			return zero, lifecycleError("wait", state, ErrRequestNotDispatched)
		case preparedRequestDispatching:
			select {
			case <-dispatchDone:
				continue
			case <-ctx.Done():
				return zero, lifecycleError("wait", r.submissionState(), context.Cause(ctx))
			}
		case preparedRequestDispatchFailed:
			return zero, dispatchErr
		case preparedRequestAbandoned:
			return zero, lifecycleError("wait", state, ErrRequestAbandoned)
		case preparedRequestDispatched, preparedRequestFinished:
			return r.waitResponse(ctx)
		default:
			return zero, lifecycleError("wait", state, ErrPreparedRequestUnavailable)
		}
	}
}

func (r *PreparedRequest[T]) waitResponse(ctx context.Context) (T, error) {
	var zero T
	responseReady := false
	select {
	case <-r.pending.done:
		responseReady = true
	case <-ctx.Done():
		if !r.conn.pendingResponded(r.idKey, r.pending) {
			return zero, lifecycleError("wait", r.submissionState(), context.Cause(ctx))
		}
	case <-r.conn.Done():
		if !r.conn.pendingResponded(r.idKey, r.pending) {
			<-r.pending.done
			break
		}
	}
	if !responseReady {
		<-r.pending.done
	}
	response, err := r.pending.result()
	if err != nil {
		r.mu.Lock()
		r.phase = preparedRequestFinished
		r.mu.Unlock()
		return zero, lifecycleError("wait", r.submissionState(), err)
	}
	return r.resolveResponse(ctx, response)
}

func (r *PreparedRequest[T]) resolveResponse(ctx context.Context, response responseEnvelope) (T, error) {
	var zero T
	for {
		r.mu.Lock()
		if r.resolved {
			result, err := r.result, r.resultErr
			r.mu.Unlock()
			return result, err
		}
		if r.phase == preparedRequestAbandoned {
			state := r.submission
			r.mu.Unlock()
			return zero, lifecycleError("wait", state, ErrRequestAbandoned)
		}
		if r.resolving {
			attempt := r.resolveAttempt
			r.mu.Unlock()
			select {
			case <-attempt:
				continue
			case <-ctx.Done():
				return zero, lifecycleError("wait", r.submissionState(), context.Cause(ctx))
			}
		}
		r.resolving = true
		r.resolveAttempt = make(chan struct{})
		attempt := r.resolveAttempt
		observer := r.observer
		typedHook := r.typedHook
		r.mu.Unlock()

		result, resultErr, retryable := r.completeResponse(ctx, response, observer, typedHook)
		if retryable {
			r.mu.Lock()
			r.resolving = false
			abandoned := r.phase == preparedRequestAbandoned
			if abandoned {
				r.resolved = true
				r.resultErr = lifecycleError("wait", r.submissionStateLocked(), ErrRequestAbandoned)
			}
			close(attempt)
			finalErr := r.resultErr
			r.mu.Unlock()
			if abandoned {
				r.releasePending(ErrRequestAbandoned)
				return zero, finalErr
			}
			return zero, resultErr
		}

		r.mu.Lock()
		if r.phase == preparedRequestAbandoned {
			result = zero
			resultErr = lifecycleError("wait", r.submissionStateLocked(), ErrRequestAbandoned)
		}
		r.resolving = false
		r.resolved = true
		r.phase = preparedRequestFinished
		r.result = result
		r.resultErr = resultErr
		close(attempt)
		r.mu.Unlock()
		r.releasePending(ErrRequestAbandoned)
		return result, resultErr
	}
}

func (r *PreparedRequest[T]) completeResponse(
	ctx context.Context,
	response responseEnvelope,
	observer ResponseObserver,
	typedHook func(context.Context, T) error,
) (T, error, bool) {
	var result T
	if err := r.conn.waitNotificationsUpTo(ctx, response.notificationWatermark); err != nil {
		if ctx.Err() != nil {
			return result, lifecycleError("wait", RequestSubmissionPossible, context.Cause(ctx)), true
		}
		return result, lifecycleError("wait", RequestSubmissionPossible, err), false
	}
	if observer != nil {
		if err := observer(ctx, publicRPCResponse(response.msg)); err != nil {
			return result, lifecycleError("observe", RequestSubmissionPossible, fmt.Errorf("acp: response observer: %w", err)), false
		}
	}
	if response.msg.Error != nil {
		return result, lifecycleError("wait", RequestSubmissionPossible, response.msg.Error), false
	}
	if len(response.msg.Result) != 0 {
		if err := json.Unmarshal(response.msg.Result, &result); err != nil {
			return result, lifecycleError("wait", RequestSubmissionPossible, &ResponseDecodeError{Err: err}), false
		}
	}
	if typedHook != nil {
		if err := typedHook(ctx, result); err != nil {
			return result, lifecycleError("hook", RequestSubmissionPossible, fmt.Errorf("acp: response hook: %w", err)), false
		}
	}
	return result, nil, false
}

func publicRPCResponse(msg anyMessage) RPCResponse {
	response := RPCResponse{
		Result: append(json.RawMessage(nil), msg.Result...),
	}
	if msg.ID != nil {
		response.RequestID = append(json.RawMessage(nil), (*msg.ID)...)
	}
	if msg.Error != nil {
		response.Error = cloneRequestError(msg.Error)
	}
	return response
}

// Abandon idempotently releases local response ownership. It never sends a
// cancellation notification and never changes a possibly submitted request
// into a safe-to-retry request.
func (r *PreparedRequest[T]) Abandon() RequestSubmissionState {
	if r == nil || r.conn == nil || r.pending == nil {
		return RequestSubmissionNotStarted
	}
	r.mu.Lock()
	state := r.submission
	if r.resolved || r.phase == preparedRequestFinished {
		r.mu.Unlock()
		return state
	}
	if r.phase != preparedRequestAbandoned {
		r.phase = preparedRequestAbandoned
		r.abandonOnce.Do(func() { close(r.abandoned) })
	}
	resolving := r.resolving
	r.mu.Unlock()
	if resolving {
		r.conn.detachPending(r.idKey, r.pending)
		return state
	}
	r.releasePending(ErrRequestAbandoned)
	return state
}

func (r *PreparedRequest[T]) releasePending(cause error) {
	r.pendingRelease.Do(func() {
		if !r.conn.abandonPending(r.idKey, r.pending, cause) {
			r.conn.finishPending(r.idKey, r.pending)
		}
	})
}

func (r *PreparedRequest[T]) submissionState() RequestSubmissionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submissionStateLocked()
}

func (r *PreparedRequest[T]) submissionStateLocked() RequestSubmissionState {
	if r.submission == RequestSubmissionPossible {
		return RequestSubmissionPossible
	}
	if r.phase == preparedRequestReady || r.phase == preparedRequestDispatching {
		return RequestSubmissionPending
	}
	return RequestSubmissionNotStarted
}

func lifecycleError(operation string, state RequestSubmissionState, err error) error {
	if err == nil {
		return nil
	}
	return &RequestLifecycleError{operation: operation, submission: state, err: err, classified: true}
}

func cloneRequestError(source *RequestError) *RequestError {
	if source == nil {
		return nil
	}
	clone := &RequestError{Code: source.Code, Message: source.Message}
	if source.Data == nil {
		return clone
	}
	encoded, err := json.Marshal(source.Data)
	if err != nil {
		return clone
	}
	_ = json.Unmarshal(encoded, &clone.Data)
	return clone
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
