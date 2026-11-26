# Connection Bounds, Lifecycle, and Advanced Usage

This guide covers the production resource bounds, lifecycle guarantees, error types, and advanced protocol helpers in `github.com/caelis-labs/acp-go-sdk`.

## Connection bounds and defaults

Every `acp.Connection` (including `AgentSideConnection` and `ClientSideConnection`) enforces finite bounds on frames, concurrency, and queues. Pass `acp.ConnectionOptions` when constructing a connection or calling `stdio.ServeAgent` / `stdio.StartClient`; zero values select production defaults (`acp.DefaultConnectionOptions()`):

| Option | Default | What it bounds |
|---|---|---|
| `MaxFrameSize` | `10 MiB` (`10 * 1024 * 1024`) | Maximum single NDJSON transport frame size |
| `MaxPendingRequests` | `1024` | Concurrent outbound in-flight requests awaiting peer responses |
| `MaxHandlerConcurrency` | `32` | Concurrently executing inbound request handlers |
| `MaxQueuedRequests` | `128` | Inbound requests queued waiting for a handler slot |
| `MaxQueuedNotifications` | `1024` | Buffered inbound notifications awaiting ordered dispatch |
| `MaxNotificationBytes` | `32 MiB` (`32 << 20`) | Total method/parameter bytes across queued and currently executing notifications |
| `MaxQueuedWrites` | `1024` | Outbound frames queued for writing |

Notification buffering is bounded by both count (`MaxQueuedNotifications`) and total method/parameter bytes (`MaxNotificationBytes`), including the notification currently executing. Exhausting either notification limit fails the connection with `acp.ErrNotificationQueueFull` instead of silently dropping accepted notifications.

## Notification admission filtering (`AcceptNotification`)

`ConnectionOptions.AcceptNotification` optionally filters unsupported notification methods before queue admission:

- A `nil` predicate accepts every notification.
- Applications must continue accepting the standard and negotiated extension notifications their role implements.
- The predicate runs directly on the reader goroutine: it must inspect only `method`, return immediately, and must not panic or call back into the connection.
- Requests, responses, and `$/cancel_request` bypass `AcceptNotification`.
- Filtering does not alter the relative order or response barriers of accepted notifications, and emits no JSON-RPC response.

## Connection lifecycle, stdio duplication, and subprocess shutdown

- **Connection ownership:** Connections own their reader and writer streams. `Close` is idempotent, `Done` closes on shutdown, `Err` exposes the immutable shutdown cause, and `Wait(ctx)` waits for all connection-owned goroutines to terminate.
- **Process stdio isolation:** `stdio.NewAgentConnection` and `stdio.ServeAgent` bind process `stdin`/`stdout` while keeping application logs on `stderr`. They use independently closable duplicates of process `stdin`/`stdout`, so connection shutdown does not close the caller's process-level descriptors. Custom stdio servers can use `stdio.DuplicateFile` before transferring a file stream to a connection.
- **Direct subprocess launch:** `stdio.StartClient` launches an explicitly named executable (`stdio.Command.Executable` and `Args`). It never invokes an implicit shell, continuously drains child `stderr`, connects the typed client, and exposes idempotent `Close` and `Wait` lifecycle semantics. `Process` retains sole ownership of the underlying command's wait operation.
- **Graceful vs immediate shutdown:** Use `Process.Shutdown(ctx)` or `ClientProcess.Shutdown(ctx)` to close protocol input first, wait for a graceful exit within the caller's deadline, and then forcefully terminate and join the owned process tree. `Close` remains the immediate-stop operation. The first `Shutdown` call owns the graceful deadline, and subsequent calls receive its cached terminal result.
- **Cross-platform process-tree containment:** Unix containment covers descendants that remain in the inherited process group. Windows children are started without creating or showing a console window and are assigned to a kill-on-close Job Object before their initial thread is resumed. Grace duration, `stderr` retention, endpoint policy, and retry decisions remain application concerns.

## Request IDs, cancellation, and NDJSON batching

- **Lossless request IDs:** Both string and numeric JSON-RPC request IDs are matched without `float64` precision loss.
- **Cancellation:** `$/cancel_request` cancels the inbound request context, and normal context cancellation (`context.Canceled` or `context.DeadlineExceeded`) is returned as JSON-RPC `-32800` (`Request cancelled`).
- **NDJSON framing and batches:** Each NDJSON line is one transport frame: a single JSON-RPC object, a non-empty batch array, or a malformed raw value. Incoming batches preserve that boundary; response-bearing entries are answered with one response array. Set `ConnectionOptions.RejectBatch = true` to reject inbound batch arrays as invalid requests before dispatching entries.
- **Profile-specific request ID validation:** `ConnectionOptions.AcceptRequestID` is an optional reader-level validator (`func(json.RawMessage) bool`). Returning `false` rejects the request with `-32600` and a `null` response ID before admission or dispatch (responses and notifications bypass it). Typed `experimental/v2` connections install `v2.IsValidRequestID`, while stable root defaults preserve existing JSON-RPC string, number, and `null` ID forms.
- **Relay forwarding:** `SendTransportFrame` forwards a complete frame for relays without flattening.

## Structured errors

- **JSON-RPC errors:** Protocol errors are `*acp.RequestError`. Use `errors.As` to inspect `Code`, `Message`, and `Data`.
- **Transport errors:** Transport read and write failures match `acp.ErrTransportFailure`. Use `errors.As` with `*acp.TransportError` to inspect `Op` (`read` or `write`) and `Cause`. For prepared requests this is independent of `RequestSubmissionState`: only `acp.RequestSubmissionNotStarted` proves that a retry cannot duplicate a remote effect (see [prepared-requests.md](prepared-requests.md)).

## Inbound context helpers and ordering hooks

- **Message metadata:** Every inbound handler context exposes `acp.InboundInfoFromContext(ctx)`, reporting `InboundRequest` vs `InboundNotification` and preserving a defensive copy of the raw request ID.
- **Connection lookup:** Generated and extension handlers can retrieve the exact current peer with `acp.AgentSideConnectionFromContext(ctx)` or `acp.ClientSideConnectionFromContext(ctx)`. This is safe when a single implementation serves multiple connections.
- **Lossless raw params:** Typed handlers that need forward-compatibility evidence from a newer peer can call `acp.InboundParamsFromContext(ctx)`. It returns a defensive copy of the original `json.RawMessage` params (including unknown nested fields), while the generated dispatcher still validates the standard typed request. Standard methods are direction-checked before decoding or callback side effects.
- **Post-response callbacks:** Handlers that must send notifications only after a successful request response has reached the wire can register one callback with `acp.AfterResponse`. The callback receives a connection-lifetime context; use it instead of retaining the completed request context.
- **Session routing hook:** For session creation, `ClientSideConnection.NewSessionWithResponseHook` lets the client install routing from the returned session ID before notifications that follow the response are dispatched. The hook must not wait for a later notification from the same connection.
- **Ordered notification handlers:** Notification handlers are invoked in wire order. They may synchronously issue reverse requests; notifications sent before the reverse response are processed in that ordered call stack before the request returns. A notification handler's context is canceled when the handler returns and must not be retained for asynchronous work.

## Tri-state patch fields, programmatic tool names, and raw forwarding

- **Nullable tri-state session info:** `SessionInfoUpdate` and the session-info `SessionUpdate` variant preserve absent, explicit `null`, and value states for `title` and `updatedAt`. Use `TitleState` or `UpdatedAtState` to inspect decoded state, and the generated `Set`, `Clear`, and `Unset` methods to construct an update. The existing pointer fields remain available for value access and direct non-nil assignment.
- **Programmatic tool-call `name`:** Tool calls expose an optional programmatic `name`. In stable v1 updates, omitted or `null` names leave an existing name unchanged; use `acp.WithStartName` and `acp.WithUpdateName` through the session-update helpers to set a name.
- **Notices and context compaction:** Stable session notices and context compaction are available when the client advertises `ClientSessionCapabilities.Notices` and `.Compaction`. Compaction `summary`, `error`, and `_meta` preserve omitted/`null`/value patch states through generated `State`, `Set`, `Clear`, and `Unset` methods.
- **Stable `SystemPrompt` on session lifecycle requests:** Stable session lifecycle requests (`new`, `load`, `resume`) accept `SystemPrompt`. Clients must first check `AgentCapabilities.SessionCapabilities.SystemPrompt`: `nil` means unsupported, while an empty capability object supports `append`; use `override` only when nested `Mode.Override` is advertised. A prompt requires an explicit mode and nonempty text content; unknown modes are preserved for forwarding. Agents own support validation and must reject unsupported modes or content before creating, loading, or resuming a session. The SDK does not apply model instructions or turn this field into permission or authorization policy.
- **Lossless `session/update` forwarding:** For transparent forwarding across schema revisions, `AgentSideConnection.SessionUpdateRaw` accepts lossless JSON params while fixing the wire method to `session/update`. It validates only the outer `sessionId` and `update` object; the caller owns the semantics of the opaque update. It does not expose arbitrary standard-method sending or bypass connection ordering and structured transport errors.
