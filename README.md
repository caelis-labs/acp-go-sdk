# ACP Go SDK

[![CI](https://github.com/caelis-labs/acp-go-sdk/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/caelis-labs/acp-go-sdk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/caelis-labs/acp-go-sdk.svg)](https://pkg.go.dev/github.com/caelis-labs/acp-go-sdk)

Product-neutral Go SDK for the
[Agent Client Protocol (ACP)](https://agentclientprotocol.com), maintained by
Caelis Labs. The module is published as
`github.com/caelis-labs/acp-go-sdk`.

The root package is stable ACP wire protocol v1 only. It contains
schema-generated wire types, typed Agent/Client dispatch, bounded bidirectional
JSON-RPC (including lossless batch frames), cancellation, and the stable stdio
NDJSON transport. It does not
contain an agent runtime, persistence, authorization, replay, UI projection, or
product-specific extensions.

The `v1` release line is the stable, production-ready ACP v1 API. It is gated
by schema reproducibility, race/static/fuzz validation, fresh-consumer builds,
and recorded bidirectional interoperability against pinned official TypeScript
and Rust SDK peers.

## Protocol provenance

| Identity | Pinned value |
|---|---|
| Wire protocol | 1 |
| Schema artifact | 1.21.0 |
| Schema tag | schema-v1.21.0 |
| Upstream commit | 272bf799f35a258c6a4107a0410ed361e83683d3 |
| schema.json SHA256 | caf62ff962ada396878372ced11efb2c6764e59d90919a38583c319948931a42 |

The exact tag object, commit, assets, and hashes are recorded in
schema/lock.json. Generated stable code uses schema/schema.json only;
schema.unstable.json and v2 schemas are not merged into this package.

Tracked official identities, including the draft v2 schema tag, are also
recorded in `upstream/lock.json`. `make verify-upstream` checks that file
against `schema/lock.json` and `interop/versions.json`. A scheduled workflow
queries GitHub releases and opens an `upstream-drift` issue when a pin is
behind.

Draft ACP v2 lives in `experimental/v2`. It is generated from
`schema-v2.0.0-alpha.3` and is not a stable API. `session/prompt` acknowledges
acceptance only; running/idle/requires_action are `session/update` state
updates. Its typed dispatch and lifecycle have Go loopback coverage; the
official TypeScript/Rust interoperability gate currently covers stable ACP v1.
Do not import this package from the stable root.

## Install

~~~bash
go get github.com/caelis-labs/acp-go-sdk@v1.3.0
~~~

## Releases

Release metadata is committed and pushed before a version tag is created. A
tag may point only to an exact commit on `main` whose complete CI workflow has
succeeded. The manual release workflow verifies both conditions before it
creates an annotated tag; it does not rebuild or modify the selected commit.

See [RELEASING.md](RELEASING.md) for the release procedure and post-publication
pkg.go.dev checks.

## Agent side

Implement the four baseline methods on acp.Agent: Initialize, NewSession,
Prompt, and Cancel. Optional methods are separate interfaces, such as
AgentLoader, AgentSessionLister, and AgentSessionConfig. If an optional
interface is omitted, inbound calls return JSON-RPC method-not-found; do not
advertise that capability from Initialize.

~~~go
connection, err := acp.NewAgentSideConnectionWithOptions(
    agent,
    os.Stdout,
    os.Stdin,
    acp.ConnectionOptions{},
)
if err != nil {
    return err
}
defer connection.Close()
return connection.Wait(ctx)
~~~

For process stdio, transport/stdio.NewAgentConnection and
transport/stdio.ServeAgent bind the same transport without placing logs on
protocol stdout. They use independently closable duplicates of process
stdin/stdout, so connection shutdown does not close the caller's process-level
descriptors. Custom stdio servers can use transport/stdio.DuplicateFile before
transferring a file stream to a Connection.

## Client side and subprocesses

Implement the baseline acp.Client methods RequestPermission and SessionUpdate;
opt into filesystem, terminal, or elicitation calls only by implementing and
advertising their dedicated optional interfaces.

transport/stdio.StartClient launches an explicitly named executable with an
argument slice. It never invokes a shell, drains child stderr, connects the
typed client, and exposes idempotent close/wait lifecycle. On Windows, ACP
children are started without creating or showing a console window. `Process`
retains sole ownership of the underlying command's wait operation.

Use `Process.Shutdown(ctx)` or `ClientProcess.Shutdown(ctx)` to close protocol
input first, wait for a graceful exit within the caller's deadline, and then
forcefully terminate and join the owned process tree. `Close` remains the
immediate-stop operation. The first `Shutdown` call owns the graceful deadline,
and later calls receive its cached terminal result. Unix containment covers
descendants that remain in the inherited process group; Windows children are
assigned to a kill-on-close Job Object before their initial thread is resumed.
Grace duration, stderr
retention, endpoint policy, and retry decisions remain application concerns.

## Resource bounds and lifecycle

Every connection has finite limits for:

- frame size;
- outstanding requests;
- concurrently running inbound handlers;
- queued requests and ordered notifications;
- queued writes and cancellation notifications.

Use ConnectionOptions to tune them. Zero values select production defaults.
Notification buffering is bounded by both count and total method/parameter
bytes, including the notification currently executing. `MaxNotificationBytes`
defaults to 32 MiB; exhausting either notification limit fails the connection
with `ErrNotificationQueueFull` instead of dropping accepted notifications.

`AcceptNotification` optionally filters unsupported notification methods before
queue admission. A nil predicate accepts every notification. Applications must
continue accepting the standard and negotiated extension notifications their
role implements. The predicate runs on the reader, must return immediately,
and must not panic or call the connection. Requests, responses, and
`$/cancel_request` bypass it. Filtering does not change the relative order or
response barriers of accepted notifications, and emits no JSON-RPC response.
Connections own their reader/writer streams, Close is idempotent, Done signals
shutdown, Err exposes its immutable cause, and Wait(ctx) waits for all
connection-owned goroutines.

Both string and numeric JSON-RPC request IDs are matched without float64
conversion. $/cancel_request cancels the inbound request context and normal
context cancellation is returned as JSON-RPC -32800.

Each NDJSON line is one transport frame: a single JSON-RPC object, a non-empty
batch array, or a malformed raw value. Incoming batches keep that boundary;
response-bearing entries are answered with one response array.
SendTransportFrame forwards a complete frame for relays without flattening.

JSON-RPC errors are *acp.RequestError; callers can use errors.As to inspect
Code, Message, and Data.

Transport read and write failures match acp.ErrTransportFailure. Use
errors.As with *acp.TransportError to inspect the operation and underlying
cause. For prepared requests this is independent of RequestSubmissionState:
only RequestSubmissionNotStarted proves that retry cannot duplicate a remote
effect.

Every inbound handler context exposes acp.InboundInfoFromContext. It reports
request versus notification and preserves the raw request ID. Generated and
extension handlers can also retrieve the exact current peer with
acp.AgentSideConnectionFromContext or acp.ClientSideConnectionFromContext;
this is safe when one implementation serves multiple connections.

Typed handlers that need compatibility evidence from a newer peer can use
acp.InboundParamsFromContext. It returns a defensive copy of the original
params, including unknown nested fields, while the generated dispatcher still
validates the standard typed request. Standard methods are direction-checked
before decoding or callback side effects.

Handlers that must send notifications only after a successful request response
has reached the wire can register one callback with acp.AfterResponse. The
callback receives a connection-lifetime context; use it instead of retaining
the completed request context.
For session creation, ClientSideConnection.NewSessionWithResponseHook lets the
client install routing from the returned session ID before notifications that
follow the response are dispatched. The hook must not wait for a later
notification from the same connection.

Notification handlers are invoked in wire order. They may synchronously issue
reverse requests; notifications sent before the reverse response are processed
in that ordered call stack before the request returns. A notification handler's
context is canceled when the handler returns and must not be retained for
asynchronous work.

SessionInfoUpdate and the session-info SessionUpdate variant preserve absent,
explicit null, and value states for title and updatedAt. Use TitleState or
UpdatedAtState to inspect decoded state, and the generated Set, Clear, and
Unset methods to construct an update. The existing pointer fields remain
available for value access and direct non-nil assignment.

For transparent forwarding across schema revisions,
AgentSideConnection.SessionUpdateRaw accepts lossless JSON params while fixing
the wire method to `session/update`. It validates only the outer `sessionId`
and `update` object; the caller owns the semantics of the opaque update. It
does not expose arbitrary standard-method sending or bypass connection
ordering and structured transport errors.

## Prepared request lifecycle

Advanced callers can reserve a bounded pending request without writing to the
transport, dispatch it under one context, and transfer response ownership to a
different context:

~~~go
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
~~~

`Dispatch` returning nil proves only that the local writer accepted the full
frame; it does not prove that the peer executed or committed the operation.
Once the writer is invoked, errors remain `RequestSubmissionPossible` even for
zero-byte or partial writes. A live or concurrently dispatching request reports
`RequestSubmissionPending`, which is also not safe to retry. Only an SDK
classification of `RequestSubmissionNotStarted` proves that future submission
is impossible; `RequestMayHaveBeenSubmitted` treats unclassified errors
conservatively.

`CancelRequest` sends at most one best-effort ACP `$/cancel_request` after a
successful dispatch and retains the original response waiter. `Abandon` only
releases local pending ownership. `DispatchOptions.Abort` is the separate
transport-revocation hook for a write that cannot be interrupted by context
cancellation; closing a shared connection can terminate other pending requests.

Response observers run after notifications received before the response have
completed and before typed decoding or public `Wait` completion. Notifications
received after the response remain gated until observation and response
completion finish. Observers must not wait for work that depends on a later
notification from the same connection.

## Generate and validate

~~~bash
make verify-schema
make verify-upstream
make check-generated
make test
make test-race
make vet
~~~

The generator is a separate module under cmd/generate. Generated files carry
DO NOT EDIT headers. make check-generated regenerates into a temporary
directory and compares every generated file.

Fuzz seed corpora for JSON messages, request IDs, unions, and framing replay
during ordinary go test. Longer fuzzing can target individual fuzz functions:

~~~bash
go test -run=^$ -fuzz=FuzzRequestID -fuzztime=30s
~~~

## Official SDK interoperability

The repository contains a deterministic four-direction interoperability
matrix against pinned official TypeScript and Rust SDK peers. A Go runner owns
all assertions; the language-specific peers are thin public-API adapters and
reserve agent stdout for ACP NDJSON.

Install Node.js and Rust through `rustup`, then run:

~~~bash
make interop
~~~

The matrix covers Go client to official SDK agent and official SDK client to
Go agent for TypeScript and Rust. Each direction exercises ordered session
updates, a reverse permission request, `session/cancel`, and the distinct
`$/cancel_request` / JSON-RPC `-32800` path. Exact SDK identities and toolchain
requirements are recorded in `interop/versions.json` and `upstream/lock.json`;
machine-readable run evidence is written under `.artifacts/interop/` and
uploaded by CI.

See `interop/README.md` for harness boundaries and scenario definitions.

## Attribution

The implementation derives its generator and connection baseline from
[coder/acp-go-sdk](https://github.com/coder/acp-go-sdk), then updates it for
the current official schema and the stricter bounds/lifecycle contract in this
repository. See NOTICE and LICENSE.
