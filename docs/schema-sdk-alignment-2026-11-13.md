# Schema and SDK alignment, 2026-11-13

This change addresses [upstream drift #41](https://github.com/caelis-labs/acp-go-sdk/issues/41).
Official tags and latest SDK releases were checked on 2026-11-13. The root
package continues to negotiate ACP wire v1; draft v2 remains isolated in
`experimental/v2`. No module release version is changed.

| Source | Previous pin | Updated pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.29.1 | [schema-v1.31.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.31.0) | 0849001ca66fb975fc01111ddab9093bc8304e98 |
| Draft v2 | schema-v2.0.0-alpha.13 | [schema-v2.0.0-alpha.15](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.15) | 0849001ca66fb975fc01111ddab9093bc8304e98 |
| TypeScript SDK | 2.4.0 | [2.6.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.6.0) | 8649e0af14cd1112bf02e5f137217f8c3fcf7e90 |
| Rust SDK | 5.0.0 | [5.2.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v5.2.0) | 91d368ad8b6761f314da9095c4a030d365c079a9 |

The snapshots are exact `schema.json` and `meta.json` release assets, with
SHA256 values matching GitHub release asset digests. Both annotated tag objects
and their peeled commits are locked. npm `gitHead` and the Rust crate's
`.cargo_vcs_info.json` match the SDK tags. Dependency locks and interop identity
assertions record the published npm integrity and Rust checksum. Cargo updates
only ACP core/derive to 5.2.0 and schema to 1.17.0; the Rust toolchain stays 1.88.0.

## Complete upstream review

Schema 1.30.0 / v2 alpha.14 stabilizes client-provided system prompts. This is
the structural addition to the non-unstable assets between the old and new
pins: five definitions, optional lifecycle fields and capability advertisement.
Stable v1 adds the field to new/load/resume; v2 adds it to new/resume and has
no load method. V2 alpha.15 also clarifies that session cancellation targets
foreground work. No existing definitions or fields are removed.

Schema 1.31.0 / alpha.15 additionally changes the separate unstable assets:
auth account descriptions, session/options and initial config option values,
and session tool-call content with clarified one-shot subagents. Plan operation
and background work documentation also changes. These are not definitions in
the pinned non-unstable assets and are not imported into either generated
surface. Upstream website, dependency and release workflow changes do not alter
the SDK's wire contract.

TypeScript 2.5 and 2.6 adopt those schema snapshots, along with development
dependency updates. Rust 5.1 adopts schema crate 1.16 and stable system prompts;
5.2 adopts 1.17 and adds the opt-in unstable session options feature. The peers
continue using the TypeScript default v1 entrypoint and Rust's explicit v1 API,
without enabling unstable protocol v2 or session options.

## Capability and compatibility contract

An omitted or null system prompt capability means unsupported; `{}` advertises
append mode. Override requires the nested `mode.override` capability. Both
profiles generate independent prompt, mode and capability types. Clients must
check support before sending a prompt; agents must reject unsupported modes
or content before performing a lifecycle operation. The SDK preserves unknown
mode strings for forwarding, and does not interpret instructions or implement
agent policy. Required mode/content fields reject omission and null; the
schema does not impose `minItems`, so applications enforce nonempty text content.

Existing request fields and methods are retained, and absent system prompts
remain omitted on the wire. Public API comparison against the previous main
reports only additive types/fields plus the expected schema provenance constant
changes. Applications should use keyed request struct literals when upgrading.
V2 cancellation remains application-owned; its updated comment does not add a
runtime scheduler or automatically cancel background work.

## Validation and scope

Local validation uses Go 1.26.8, Node 24.19.0 and Rust 1.88.0. `make check test-race` covers both Go modules, schema checksums, lock consistency,
deterministic generation, formatting, vet, tests and race detection. Additional
checks cover example builds, a fresh consumer module, public API comparison,
fuzz corpus replay, 15 seconds per fuzz target, and the CI script tests.

`make interop` runs Rust formatting, Clippy and tests plus all 12 real SDK
cases (four directions, each with core, session cancellation and request
cancellation). Each client checks system prompt support and sends an append
text prompt on session/new; each agent verifies the mode and content before
creating the session. Core evidence includes `system-prompt:ok`. This proves
stable v1 stdio wire handling, not model behavior. Override and load/resume
payloads have Go regression coverage, not official peer lifecycle coverage.
Draft v2 has Go tests, without an official-SDK v2 interop claim.

The local container needs a temporary Linux subreaper for process-tree tests
so orphan descendants are reaped; no tests are excluded. The consumer uses
`GOFLAGS=-buildvcs=false` because the parent Git directory is sandboxed.
Golangci-lint 2.11.4 uses the checksum-verified official binary after its source
build encounters a blocked dependency download. Remote CI validates Go
1.23/1.25, native Windows, clean-commit interop evidence and `quality`; local
results alone do not establish those checks. This change does not publish a release.
