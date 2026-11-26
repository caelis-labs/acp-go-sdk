# Prepared Request Lifecycle

Advanced callers (such as proxies, relays, or admission controllers) can reserve a bounded pending request slot without writing to the transport, dispatch the request under one context, and wait for the peer response under a different context using `acp.PrepareClientRequest` or `acp.PrepareAgentRequest`.

## Example

```go
request, err := acp.PrepareClientRequest[acp.PromptResponse](
	connection,
	acp.AgentMethodSessionPrompt,
	params,
)
if err != nil {
	return err
}
defer request.Abandon()

if err := request.ObserveResponse(func(ctx context.Context, response acp.RPCResponse) error {
	// response.Result is available before typed decoding. JSON-RPC failures
	// are reported through response.Error as *acp.RequestError.
	return updateLocalAdmission(ctx, response)
}); err != nil {
	return err
}

if err := request.Dispatch(dispatchCtx, acp.DispatchOptions{
	Abort: func(error) error { return connection.Close() },
}); err != nil {
	if state, ok := acp.RequestSubmissionStateOf(err); ok &&
		state == acp.RequestSubmissionNotStarted {
		// The writer was never invoked and can no longer be invoked.
	}
	return err
}

response, err := request.Wait(producerCtx)
```

## Submission states and retry safety

- `Dispatch` returning `nil` proves only that the local writer accepted the full frame; it does not prove that the peer executed or committed the operation.
- Once the writer is invoked, errors remain `acp.RequestSubmissionPossible` even for zero-byte or partial writes.
- A live or concurrently dispatching request reports `acp.RequestSubmissionPending`, which is also not safe to retry.
- Only an SDK classification of `acp.RequestSubmissionNotStarted` proves that future submission is impossible.
- `acp.RequestMayHaveBeenSubmitted` treats unclassified errors conservatively.

## Cancellation, abandonment, and abort hooks

- `CancelRequest` sends at most one best-effort ACP `$/cancel_request` after a successful dispatch and retains the original response waiter.
- `Abandon` only releases local pending ownership.
- `DispatchOptions.Abort` is the separate transport-revocation hook for a write that cannot be interrupted by context cancellation; closing a shared connection can terminate other pending requests.

## Response observer ordering guarantees

Response observers registered via `ObserveResponse` run after notifications received before the response have completed and before typed decoding or public `Wait` completion. Notifications received after the response remain gated until observation and response completion finish. Observers must not wait for work that depends on a later notification from the same connection.
