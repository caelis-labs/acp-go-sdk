# Changelog

All notable changes to this project are documented in this file.

## [v1.2.0] - 2026-09-07

### Fixes

- Batch `AfterResponse` callbacks run only after the complete response array
  is written. A bounded callback worker allows reverse requests without
  blocking the receive loop or starving single-worker request handling.
- The upstream drift workflow executes a built binary so the drift exit code
  reaches Issue creation and updates.
- Experimental v2 dispatch preserves structured JSON-RPC errors and cancellation
  codes, rejects request/notification mismatches before invoking handlers, and
  supports elicitation creation/completion and all declared outbound methods.
- Windows stdio retains process handles across Job Object termination and waits
  for the active process count to reach zero before reporting tree shutdown.
- Protocol routing accepts stable v1 initialize envelopes and locks the first
  initialization attempt, rejecting concurrent or repeated initialization.

### Experimental ACP v2

- Added `experimental/v2`, generated from pinned `schema-v2.0.0-alpha.3`.
  The package is draft-only and is not part of the stable root API. It
  implements initialize version pinning, session new/resume/list/close,
  prompt-acceptance ACK, and `state_update` session updates.

### JSON-RPC transport

- Ingress now classifies each NDJSON value as a single JSON-RPC object, a
  non-empty batch, or a malformed raw value. Incoming batches are dispatched
  as logical messages but answered with one response array, and
  `SendTransportFrame` forwards a complete frame so relays can preserve the
  batch boundary.

### Maintenance

- Pinned official Rust SDK interoperability to `v2.1.0`.
- Added `upstream/lock.json` and a scheduled GitHub Action that opens an
  `upstream-drift` issue when official schema or SDK releases move ahead of
  the committed pins.

## [v1.1.0] - 2026-08-29

Formal stable release of the prepared-request lifecycle, response/notification
ordering, lossless compatibility primitives, and cross-platform stdio process
lifecycle introduced across the v1.1.0 release candidates. Code behavior is
unchanged from v1.1.0-rc.4.

## [v1.1.0-rc.4] - 2026-08-28

### Typed dispatch and lossless compatibility

- Generated standard-method dispatchers now reject request/notification
  direction mismatches before parameter decoding, cancellation-state changes,
  or user callbacks. Extension handlers retain their explicit raw-direction
  policy.
- Added `InboundParamsFromContext`, which gives typed and extension handlers a
  defensive copy of the lossless inbound JSON-RPC params without introducing a
  second ingress path.
- Added `AgentSideConnection.SessionUpdateRaw`, a fixed-method escape hatch for
  forwarding future `session/update` variants and content blocks. It validates
  the outer session notification envelope while leaving the update union
  opaque.
- Typed Agent and Client peers are now completely initialized before receive
  workers start, so a callback handling an immediately available first frame
  can safely issue a reverse call through its current-connection context.

### Stdio process lifecycle

- Added `Process.Shutdown(ctx)` and `ClientProcess.Shutdown(ctx)` for graceful
  stdin/connection closure followed by deadline-bounded escalation and waiter
  joining.
- ACP subprocesses now start in an SDK-owned Unix process group or a Windows
  Job Object. Immediate close, shutdown escalation, start-context cancellation,
  and post-exit cleanup terminate inherited descendants without transferring
  ownership of the command's single `Wait` call.
- Windows children are created suspended, assigned to a kill-on-close Job
  Object, and only then resumed, eliminating the post-start containment race.
- Added graceful EOF, forced process-tree, repeated lifecycle, and concurrent
  `Close`/`Shutdown`/`Wait` coverage, including native platform assertions.

### Release engineering and documentation

- Added an exact-commit manual release workflow that creates an annotated tag
  only after the selected `main` commit has a successful push-triggered CI run.
- Updated official GitHub Actions to their Node.js 24-based stable major
  versions.
- Documented the push, CI, tag, Go proxy, and pkg.go.dev publication sequence.
- Improved the package synopsis and project metadata guidance for ACP and
  acp-go-sdk discovery on pkg.go.dev and source hosts.

## [v1.1.0-rc.3] - 2026-08-28

### Windows stdio

- File duplication now obtains the source descriptor through
  `SyscallConn.Control`. This rejects closed files on Windows instead of
  interpreting their `-1` descriptor as the current-process pseudo handle,
  and protects the source file from concurrent close while it is duplicated.

## [v1.1.0-rc.2] - 2026-08-28

### Handler context and wire semantics

- Added `InboundInfoFromContext` so existing raw, extension, and generated
  handlers can distinguish requests from notifications and inspect the
  lossless raw JSON-RPC request ID without adding a second ingress path.
- Added current-connection accessors for agent-side and client-side handlers.
  Shared Agent or Client implementations can now issue reverse calls through
  the connection that delivered the current message instead of retaining a
  global peer.
- Generated session-info updates now preserve the protocol's
  absent/null/value distinction for `title` and `updatedAt`. Presence state and
  generated set/clear/unset methods are available without changing the
  existing pointer fields or wire schema.

### Transport and stdio

- Added `ErrTransportFailure` and `TransportError`, including stable read/write
  operation classification and ordinary error unwrapping. Prepared requests
  continue to report submission certainty independently.
- `transport/stdio.NewAgentConnection` now gives the SDK independently
  closable duplicates of process stdin/stdout. The original process-level
  descriptors remain caller-owned; `DuplicateFile` exposes the same narrow,
  cross-platform primitive to custom stdio servers.

## [v1.1.0-rc.1] - 2026-08-25

### Ordering and stdio

- Changed notification progress signaling from a single buffered token to a
  broadcast generation channel. Concurrent prepared responses waiting for the
  same earlier notification now all resume without serializing response waits.
- ACP child processes started through `transport/stdio` now use
  `HideWindow` and `CREATE_NO_WINDOW` on Windows. Post-exit pipe cleanup is
  idempotent across Windows and Unix, while `Process` remains the sole owner of
  the child command's `Wait` call.

## [v1.1.0-rc] - 2026-08-25

### Prepared request lifecycle

- Added a product-neutral, bounded `PreparedRequest[T]` lifecycle that
  separates request preparation, dispatch, response waiting, cooperative
  cancellation, and local abandon.
- Added a writer-admission linearization point and conservative
  `RequestSubmissionState` errors. Cancellation before writer admission proves
  that no request bytes can be written; invoking the writer permanently marks
  the request as possibly submitted, including zero-byte and partial failures.
  Live or concurrently dispatching requests are classified as pending, never
  as safe to retry.
- Added an optional once-only dispatch abort callback for revoking the exact
  transport used by a blocked write. Transport abort remains distinct from ACP
  `$/cancel_request` and local `Abandon`.
- Added decode-independent response observers covering successful results and
  JSON-RPC errors before typed `Wait` completion. Successful response decode
  failures now have a dedicated `ResponseDecodeError` in the prepared API and
  are never classified as safe to retry.
- Connection shutdown now atomically drains all pending requests, including
  prepared requests without an active waiter.

### Ordering and compatibility

- Added bounded after-response callbacks and response hooks that preserve
  causal delivery between a request response and immediately following
  notifications, including session-scoped routing after `session/new`.
- Prepared response observers reuse the same response-delivery and notification
  ordering path; no alternate JSON-RPC ingress or scheduler was added.
- Existing `SendRequest` and `SendRequestWithResponseHook` now share the new
  lifecycle core while retaining their public success-hook and error behavior.

## [v1.0.1] - 2026-08-24

First stable release from the rebuilt public repository history. It supersedes
v1.0.0 without protocol or API changes.

### Protocol and API

- Stable root package generated deterministically from the pinned official ACP
  v1 schema release `schema-v1.21.0`.
- Schema-formatted integer widths, exact string/number JSON-RPC request IDs,
  lossless `_meta` values, and raw forward-compatible union variants.
- Typed bidirectional Agent and Client dispatch with explicit optional
  capability interfaces.
- JSON-RPC errors expose code, message, and data through `errors.As`.

### Runtime safety

- Bounded frames, pending requests, handlers, notifications, writes, and
  cancellation queues.
- Ordered notifications, concurrent requests in both directions,
  `$/cancel_request`, JSON-RPC `-32800`, idempotent `Close`, and
  context-bounded `Wait`.
- Stable stdio NDJSON transport with explicit executable spawning and drained
  child stderr; protocol stdout remains log-free.

### Release evidence

- Race, static analysis, schema checksum, generator reproducibility, fuzz
  corpus, example build, and fresh consumer-module gates.
- Twelve-case, four-direction interoperability matrix against the pinned
  official TypeScript SDK `v1.4.0` and Rust SDK `v2.0.0`, covering core flow,
  session cancellation, and request cancellation.
