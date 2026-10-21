# TypeScript SDK 2.0.0 alignment

Issue [#29](https://github.com/caelis-labs/acp-go-sdk/issues/29) tracks the
TypeScript package update from 1.9.0 to
[2.0.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.0.0).
The official tag and npm `gitHead` both resolve to
`c2b1a8633745dd859a2732b4e17d5ed61e869bb6`; the npm artifact integrity is recorded
in `interop/versions.json` and `package-lock.json`. `upstream/lock.json`, the peer
identities and dependency-lock assertions use the same release. Rust remains
3.4.0 with toolchain 1.88.0.

## API and protocol review

Upstream removed the legacy connection classes and split stable/unstable
entrypoints. The existing peer already uses `agent()` / `client()` apps with
peer-context `request()` / `notify()`, so no connection-class migration is needed.
The tested session/list, notice, compaction, permission and cancellation features
are available in the default stable v1 entrypoint. Keep that entrypoint for apps,
types and method constants; no unstable import is required for this harness.

SDK package major 2 does not change the default ACP wire major: it remains 1.
Stable schema-v1.26.0, isolated schema-v2.0.0-alpha.9, all generated files and Go
production sources are unchanged. The pinned public API comparison against main
`e5697e06036d1923546f29d794c553b7758f6c24` is empty.

## Local validation

Validated on macOS arm64 with Go 1.26.9, Node 23.11.0, TypeScript 6.0.2,
Rust 1.88.0 and golangci-lint 2.11.4:

- `make check test-race`: both Go modules, vet, schema checksums, upstream lock
  consistency, deterministic stable/v2 generation, formatting and whitespace.
- golangci-lint in both Go modules: zero issues.
- `make interop`: all 12 real official-SDK scenarios, four directions times
  core/session-cancel/request-cancel; npm clean install/build and Rust
  build/fmt/Clippy/test also pass (the thin Rust peer has no unit tests).
- All core traces retain `empty-params:ok`, reverse permission and ordered display
  updates, including absent patches, empty summary replacement and explicit clears.
- Fuzz corpus replay and 15 seconds each for RequestID, GeneratedUnions and
  ConnectionFraming with two workers.
- 110 Node script tests, example builds and a fresh local consumer module.
- Repository-pinned API comparison tool: no public API differences from main.
- Remote upstream drift check: pins match official releases.

The interop runner writes exact toolchain, dependency, schema and asserted trace
identities to `.artifacts/interop/evidence.json`. Its report states the tested Git
commit and whether the source tree was dirty; use a clean committed run for
candidate evidence.

## Coverage boundary

This matrix proves stable ACP v1 interoperability in both Go roles against the
official TypeScript 2.0.0 and Rust 3.4.0 SDKs. It does not establish official SDK
v2 or unstable-feature interoperability. Rust's public transport canonicalizes
outgoing null params to omitted. Native Windows and the CI Go 1.23/1.25 matrix
were not run locally. No remote CI, release acceptance, push, PR or publication
is part of this local change.
