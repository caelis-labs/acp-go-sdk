# Official SDK alignment, 2026-10-22

Issue [#31](https://github.com/caelis-labs/acp-go-sdk/issues/31) tracks the
TypeScript 2.0.0 → 2.0.1 and Rust 3.4.0 → 3.5.0 releases.

## Provenance and dependency locks

- TypeScript tag `v2.0.1` and published npm `gitHead` both resolve to
  `2ab61518f7079ef5f6c7a961f0ebcab23ef5f59d`. The published npm SHA512 integrity
  is mirrored in `interop/versions.json`, the dependency test and the npm lock.
- Rust tag `v3.5.0` resolves to
  `305aefda6fbe88be6ae8b9088003e383f909b135`. Cargo resolves and verifies the
  registry artifact checksum recorded in `interop/versions.json` and the test.
  The SDK and its derive crate both move to 3.5.0; other locked dependencies,
  including schema crate 1.12.0, remain unchanged. Rust remains on 1.88.0.

## Upstream change review

The [TypeScript comparison](https://github.com/agentclientprotocol/typescript-sdk/compare/v2.0.0...v2.0.1)
changes HTTP stream session-conflict handling, tests and release metadata. It
isolates session failures from the shared HTTP transport. Our stdio peer keeps
using the default stable v1 entrypoint and `agent()` / `client()` APIs.

The [Rust comparison](https://github.com/agentclientprotocol/rust-sdk/compare/v3.4.0...v3.5.0)
adds connection termination observation, typed lower-version initialization
outcomes for experimental v2, and redacted protocol warning logs. Termination
also settles pending requests when a driver ends, so the existing bidirectional
core and cancellation scenarios must be rerun against the actual new artifact.
No peer API migration is needed. The new v2 APIs remain behind upstream's
unstable feature and are outside this stable v1 matrix.

Neither release updates the pinned protocol schemas. Stable schema-v1.26.0,
isolated schema-v2.0.0-alpha.9, generated protocol files and production Go sources
remain unchanged. This update does not claim HTTP or experimental v2 interop.

## Validation

Run `make check test-race`, `make interop`, example builds, the fresh consumer
smoke test, fuzz corpus replay and the repository-pinned public API comparison.
The interop harness records all 12 scenarios (four directions, each with core,
session-cancel and request-cancel) in `.artifacts/interop/evidence.json`.
CI uploads that report with the exact npm and Cargo locks.

The Linux cloud workspace requires `GOFLAGS=-buildvcs=false` for local Go builds:
its parent workspace Git directory is sandboxed. Git commit identities in interop
evidence are collected separately. Remote CI uses the unchanged workflow,
including Go 1.23/1.25, Windows stdio, static analysis and the full interop matrix.
