# TypeScript SDK 2.0.2 alignment

Issue [#33](https://github.com/caelis-labs/acp-go-sdk/issues/33) tracks the
official TypeScript SDK 2.0.1 → 2.0.2 update.

## Provenance

The [v2.0.2 release](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.0.2)
and the published npm `gitHead` resolve to
`f2f218d314ee8f42c37c466b55d2b917ddc0006c`. The npm artifact integrity is
`sha512-jLywRB42hZMRMc4aUgt0uRjkY9FQnMdybv4XgLl+ddi75QuJn8Vbl0+CHcVHJDC0+WDfi1KQXRWtHaR5vp6gHw==`.
These identities are mirrored in the upstream lock, interop versions, npm lock
and dependency assertions. Both peer roles advertise 2.0.2.

## Compatibility review

The [tag comparison](https://github.com/agentclientprotocol/typescript-sdk/compare/v2.0.1...v2.0.2)
contains HTTP/SSE fixes, their tests, CI and release metadata. SSE accepts bare
CR and mixed line endings; request-scoped HTTP POST failures no longer tear
down an otherwise usable connection, and diagnostic response bodies are bounded.
The stdio peer continues to use the default stable ACP v1 entrypoint and the
same `agent()` / `client()` APIs. No peer API migration is required.

Upstream's latest schema releases remain `schema-v1.26.0` and
`schema-v2.0.0-alpha.9`; Rust remains at 3.5.0. Schema locks, assets, generated
files and the public Go API are unchanged. This update covers stable v1 stdio
interop; it does not establish HTTP/SSE or experimental v2 interoperability.

## Validation

Run `make check test-race`, `make interop`, example builds, fresh consumer smoke,
fuzz corpus replay and the repository-pinned public API comparison. The full
interop matrix records 12 cases (four directions, each with core,
session-cancel and request-cancel) in `.artifacts/interop/evidence.json`.
CI uploads this evidence together with the npm and Cargo locks and also runs
Go 1.23/1.25, static analysis and Windows stdio checks.
