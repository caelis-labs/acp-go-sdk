# Changelog

All notable changes to this project are documented in this file.

## Unreleased

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
