# ACP Go SDK

[![CI](https://github.com/caelis-labs/acp-go-sdk/actions/workflows/ci.yml/badge.svg?event=pull_request)](https://github.com/caelis-labs/acp-go-sdk/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/caelis-labs/acp-go-sdk.svg)](https://pkg.go.dev/github.com/caelis-labs/acp-go-sdk)

**Build [Agent Client Protocol (ACP)](https://agentclientprotocol.com) agents and clients in Go.**
Connect your agent to ACP clients, or add ACP agent support to your editor,
terminal, or application with typed APIs and stdio subprocess management.

Maintained by Caelis Labs and listed in [ACP's community libraries](https://agentclientprotocol.com/libraries/community#go).
Use it independently of Caelis or any particular agent framework.

- **Both sides of ACP:** implement an [agent](#agent-side), build a
  [client](#client-side-and-subprocesses), and stream session updates with typed Go APIs.
- **Stable protocol, pinned schema:** ACP wire protocol v1, generated from
  official `schema-v1.23.0`. [Trace every generated type to its source](#protocol-provenance).
- **Tested across languages:** a [four-direction interoperability matrix](#official-sdk-interoperability)
  checks Go clients and agents against the official TypeScript and Rust SDKs.
- **Predictable resource and process handling:** bounded queues, ordered
  notifications, cancellation, and [stdio shutdown with process-tree cleanup](#client-side-and-subprocesses).

[Install](#install) · [Run the examples](#run-an-agent-and-client) ·
[API reference](https://pkg.go.dev/github.com/caelis-labs/acp-go-sdk) ·
[Compatibility and scope](#compatibility-and-scope)

## Install

Requires **Go 1.23 or later**.

<!-- x-release-please-start-version -->
~~~bash
go get github.com/caelis-labs/acp-go-sdk@v1.4.0
~~~
<!-- x-release-please-end -->

Import the root package as `acp`:

~~~go
import acp "github.com/caelis-labs/acp-go-sdk"
~~~

The Go module release, official schema version, and negotiated wire protocol
version are separate identities. The install command pins the SDK release;
`schema-v1.23.0` describes the schema used to generate its ACP v1 types.

## Run an agent and client

Run a complete local conversation with only Go installed. The sample agent
returns a fixed greeting, so no model account, API key, Node.js, or Rust is needed.

~~~bash
git clone https://github.com/caelis-labs/acp-go-sdk.git
cd acp-go-sdk
go build -o .artifacts/minimal-agent ./example/minimal-agent
go run ./example/client -agent ./.artifacts/minimal-agent
~~~

Expected output:

~~~text
Hello from the minimal Go ACP agent.
Stop reason: end_turn
~~~

On Windows, build `.artifacts/minimal-agent.exe` and pass that path to `-agent`.
These commands use the repository checkout. The installation command above is
for applications consuming a published module release.

| Start here | What it demonstrates |
|---|---|
| [Minimal agent](example/minimal-agent/main.go) | Implement `Initialize`, `NewSession`, `Prompt`, and `Cancel`; stream a response over stdio. |
| [Client example](example/client/main.go) | Launch an agent, negotiate ACP v1, create a session, send a prompt, print streamed text, and shut down the child. |

The client example cancels permission requests and advertises no filesystem or
terminal capabilities. To connect it to another installed ACP agent, use
`-agent /path/to/executable`; place the agent's arguments after `--`.

## Agent side

Use the [minimal agent](example/minimal-agent/main.go) as a runnable starting point.
Implement the four baseline methods on `acp.Agent`: `Initialize`, `NewSession`,
`Prompt`, and `Cancel`. Optional methods are separate interfaces, such as
AgentLoader, AgentSessionLister, and AgentSessionConfig. If an optional
interface is omitted, inbound calls return JSON-RPC method-not-found; do not
advertise that capability from Initialize.

~~~go
connection, err := stdio.NewAgentConnection(agent, acp.ConnectionOptions{})
if err != nil {
    return err
}
defer connection.Close()
return connection.Wait(ctx)
~~~

Import `github.com/caelis-labs/acp-go-sdk/transport/stdio` for this example.
`stdio.NewAgentConnection` and `stdio.ServeAgent` bind process stdio;
application logs belong on stderr. They use independently closable duplicates
of process stdin/stdout, so connection shutdown does not close the caller's
process-level descriptors. Custom stdio servers can use `stdio.DuplicateFile`
before transferring a file stream to a Connection.

## Client side and subprocesses

Use the [client example](example/client/main.go) for the complete connection flow.
Implement the baseline `acp.Client` methods `RequestPermission` and `SessionUpdate`;
opt into filesystem, terminal, or elicitation calls only by implementing and
advertising their dedicated optional interfaces.

`transport/stdio.StartClient` launches an explicitly named executable with an
argument slice. It never invokes a shell, drains child stderr, connects the
typed client, and exposes idempotent close/wait lifecycle. On Windows, ACP
children are started without creating or showing a console window. `Process`
retains sole ownership of the underlying command's wait operation.

The client starts an executable directly and exposes typed ACP calls through
`process.Connection`:

~~~go
process, err := stdio.StartClient(ctx, client, stdio.Command{
    Executable: agentPath,
    Args:       agentArgs,
}, acp.ConnectionOptions{})
if err != nil {
    return err
}
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := process.Shutdown(shutdownCtx); err != nil {
        log.Print(err)
    }
}()
~~~

Then call `Initialize` and verify the returned protocol version before
`NewSession` and `Prompt`. Handle `SessionUpdate` callbacks to display streamed
output. The [full example](example/client/main.go) includes those calls, error
handling, an absolute session working directory, and request deadlines.

Use `Process.Shutdown(ctx)` or `ClientProcess.Shutdown(ctx)` to close protocol
input first, wait for a graceful exit within the caller's deadline, and then
forcefully terminate and join the owned process tree. `Close` remains the
immediate-stop operation. The first `Shutdown` call owns the graceful deadline,
and later calls receive its cached terminal result. Unix containment covers
descendants that remain in the inherited process group; Windows children are
assigned to a kill-on-close Job Object before their initial thread is resumed.
Grace duration, stderr retention, endpoint policy, and retry decisions remain
application concerns.

## Compatibility and scope

Use this SDK when you want to connect Go applications to the ACP ecosystem
while keeping control of your model provider, application state, and permission
policy. Typed dispatch and transport lifecycle are supplied by the SDK; your
application implements the agent or client behavior.

| Surface | Contract |
|---|---|
| Root `acp` package | Stable ACP wire protocol v1; generated types, Agent/Client dispatch, and concurrent bidirectional JSON-RPC. |
| `transport/stdio` | Stable NDJSON transport, direct executable launch, stderr draining, and owned subprocess lifecycle. |
| `experimental/v2` | Separate draft API generated from a pinned `schema-v2*` release; may change incompatibly. |

Optional capabilities are opt-in: implement their interfaces and advertise
only what your application supports. Omitted capabilities mean unsupported.
The SDK does not supply a model runtime, persistence, authorization policy,
or unrestricted filesystem or terminal handlers. HTTP/SSE and WebSocket draft
behavior are outside the stable transport contract.

The stable v1 release line is validated through reproducible generation,
race/static checks, fuzzing, fresh-consumer builds, native Windows stdio tests,
and the official SDK interop matrix. See [the CI workflow](.github/workflows/ci.yml)
and [release gates](RELEASING.md) for the checks and their scope.

## Official SDK interoperability

The repository contains a deterministic four-direction interoperability
matrix against pinned official TypeScript and Rust SDK peers. A Go runner owns
all assertions; the language-specific peers are thin public-API adapters and
reserve agent stdout for ACP NDJSON.

To run the conformance tests, install Node.js 24 and use `rustup` to install
the Rust toolchain pinned in [rust-toolchain.toml](interop/peers/rust/rust-toolchain.toml).
Then run:

~~~bash
make interop
~~~

| Client | Agent | Scenarios |
|---|---|---|
| Go | Official TypeScript SDK | Core conversation, session cancellation, request cancellation |
| Official TypeScript SDK | Go | Core conversation, session cancellation, request cancellation |
| Go | Official Rust SDK | Core conversation, session cancellation, request cancellation |
| Official Rust SDK | Go | Core conversation, session cancellation, request cancellation |

These **12 cases** check ordered session updates, a reverse permission request,
`session/cancel`, and the distinct `$/cancel_request` / JSON-RPC `-32800` path.
Exact SDK identities and toolchain requirements are recorded in
[interop/versions.json](interop/versions.json) and [upstream/lock.json](upstream/lock.json).
CI uploads a machine-readable report with the tested commit, dependency pins,
and event traces; local runs write it to `.artifacts/interop/evidence.json`.
The matrix covers these stable v1 scenarios, not every optional capability or
every third-party agent.

See `interop/README.md` for harness boundaries and scenario definitions.

## Protocol provenance

| Identity | Pinned value |
|---|---|
| Wire protocol | 1 |
| Schema artifact | 1.23.0 |
| Schema tag | schema-v1.23.0 |
| Upstream commit | 6d08f412a7a1370d3cc9a124e3be3d6acf92641e |
| schema.json SHA256 | 3c17bd6385d90cf672d8a661fddc359d73422cf8b8ce6865213d25cfd4c0eca7 |

The exact tag object, commit, assets, and hashes are recorded in
schema/lock.json. Generated stable code uses schema/schema.json only;
schema.unstable.json and v2 schemas are not merged into this package.

Tracked official identities, including the draft v2 schema tag, are also
recorded in `upstream/lock.json`. `make verify-upstream` checks that file
against `schema/lock.json` and `interop/versions.json`. A scheduled workflow
queries GitHub releases and opens an `upstream-drift` issue when a pin is
behind.

Draft ACP v2 lives in `experimental/v2`. It is generated from
`schema-v2.0.0-alpha.5` and is not a stable API. `session/prompt` returns the
required `messageId` of the user message inserted into the conversation;
the matching user-message update may arrive before or after that response.
Running/idle/requires_action are `session/update` state updates. Its typed
dispatch and lifecycle have Go loopback coverage; the official TypeScript/Rust
interoperability gate currently covers stable ACP v1.
Do not import this package from the stable root.

Tool calls expose an optional programmatic `name`. In stable v1 updates, omitted
or null names leave an existing name unchanged; `WithStartName` and
`WithUpdateName` set a name through the session-update helpers. In v2,
tool-call updates distinguish omission, explicit null (clear), and a string
(replace) through `NameState`, `SetName`, `ClearName`, and `UnsetName`.

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

## Releases

Release Please maintains the version, changelog, and installation example in a
Release PR. A maintainer approves its complete CI before merging. Publication
verifies that the merged release contains exactly the tested Git tree before
creating an immutable tag and GitHub Release. Ordinary PRs do not need to track
every main update; release version bumps always require the full matrix,
including race checks, Windows stdio, and official SDK interoperability.

See [RELEASING.md](RELEASING.md) for approval, validation, publication and recovery,
and [v1.4.0 migration notes](docs/upgrading-to-v1.4.0.md) for the protocol upgrade.

## Attribution

The implementation derives its generator and connection baseline from
[coder/acp-go-sdk](https://github.com/coder/acp-go-sdk), then updates it for
the current official schema and the stricter bounds/lifecycle contract in this
repository. See NOTICE and LICENSE.
