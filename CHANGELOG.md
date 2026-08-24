# Changelog

All notable changes to this project are documented in this file.

## Unreleased

- Added bounded after-response callbacks and response hooks that preserve
  causal delivery between a request response and immediately following
  notifications, including session-scoped routing after `session/new`.

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
