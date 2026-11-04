# Schema and SDK alignment, 2026-11-04

This change addresses [upstream drift #38](https://github.com/caelis-labs/acp-go-sdk/issues/38).
The latest official tagged releases were checked on 2026-11-04. The stable
root still implements ACP wire v1; the module version is maintained separately
by Release Please.

| Source | Previous pin | Updated pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.27.0 | [schema-v1.29.1](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.29.1) | 0fc18323a9fdad4e5b4a20ec2f1645dde843fd9b |
| Experimental v2 | schema-v2.0.0-alpha.10 | [schema-v2.0.0-alpha.13](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.13) | 0fc18323a9fdad4e5b4a20ec2f1645dde843fd9b |
| TypeScript SDK | 2.1.0 | [2.4.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.4.0) | 8d953276e54f8fd60108d6ffca3aefc4c83644a4 |
| Rust SDK | 3.6.0 | [5.0.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v5.0.0) | 1b1399ab4a281ea6fccbdc3722a64fe35c816399 |

The schema snapshots are exact release assets. Their locks record annotated
tag objects, peeled commits and SHA256 values, checked against GitHub's asset
digests. The npm package's `gitHead` and Rust crate's `.cargo_vcs_info.json`
match their tagged commits. The dependency locks, interop identities and
assertions agree on npm integrity and crate checksum. Cargo changes only the
SDK, derive and schema crates (to 5.0.0, 5.0.0 and 1.15.1); Rust stays on 1.88.0.

## Stable compatibility and official peers

The stable schema has documentation changes, including allowing non-ISO cost
units such as credits. Its structure and constraints are unchanged. Existing
Go types and methods are retained. `ConnectionOptions.RejectBatch` is an
additive transport option, defaulting to false. Stable v1 retains existing
batch behavior. Schema provenance constants change to the new release.

TypeScript 2.2–2.4 updates the schema, v2 helpers, and request cancellation
waiting behavior. The peer still uses the stable default entrypoint with
`agent()` / `client()`. Rust 4/5 reorganizes versioned APIs, removes SDK batch
handling, and changes transport/error/ID internals. Its peer now explicitly
uses `Agent.v1()`, `Client.v1()`, and `v1::{schema, ConnectionTo, Responder}`.
Neither peer enables draft v2 or MCP/HTTP extensions. Upstream package major
versions do not change the negotiated ACP wire version.

## Experimental v2 migration

All changes below stay in `experimental/v2` and its generator:

- Background work snapshots on running, idle and requires-action states
  reference terminals or tool calls, preserving unknown future work types.
  Tool calls gain `paused`; tool/terminal updates gain nullable capability
  patches advertising per-entity cancellation.
- `CancelToolCall` and `CancelTerminal` use typed requests and optional agent
  interfaces. An absent implementation returns `-32602`; applications own
  entity tracking, capability checks and cancellation policy.
- Typed v2 connections reject each JSON-RPC array as one invalid request
  before dispatch. Raw connections using `ProtocolRouter` must explicitly set
  `RejectBatch`. The removed `ProtocolLevelNotification` type is no longer
  generated from the pinned schema.
- Error envelope IDs become `*RequestId`, allowing null only on errors and
  retaining integer/string precision. Error data becomes `*ErrorData`, with
  defined protocol-version/session shapes and a lossless arbitrary-JSON
  fallback. Elicitation content also preserves arbitrary JSON, including null.
- Elicitation form schemas now require `type: "object"` and `properties`.
  Supply both when using form constructors; the old empty-schema defaults
  are removed.
- Prompt arrays must contain content. Select lists must contain at least one
  value, including after invalid entries are removed from a group. Invalid
  select options are omitted from recoverable display lists.
- Usage cost tracks omitted/null/value patches. Session directories use the
  schema's explicit default-on-null hint, replacing the earlier description
  heuristic. Currency strings can use units such as credits; annotation
  `lastModified` no longer carries JSON Schema's restrictive date-time format.

Experimental API changes are intentional and outside the stable v1 guarantee.
No v1 unstable definitions or v2 wire types enter the stable root.

## Validation

Local validation uses Go 1.26.8, Node 24.19.0 and Rust 1.88.0. It includes
`make check test-race`, examples, a fresh local consumer, the pinned public API
diff against v1.5.2, fuzz corpus replay and 15 seconds each of all three fuzz
targets. The API guard accepts only provenance-value changes and isolated
experimental changes; the stable option is additive.

`make interop` passes Rust formatting, Clippy and tests, then all 12 real
official-SDK cases: four directions with core, session cancellation and
request cancellation. `.artifacts/interop/evidence.json` records identities,
toolchains, checkout cleanliness and traces. Local development evidence may
be dirty; CI reruns from the immutable clean candidate and uploads evidence.
This proves stable ACP v1 stdio interoperability. V2 has Go decoding and
connection tests, without a claim of official-SDK v2 interoperability.

The cloud consumer uses `GOFLAGS=-buildvcs=false` because its parent Git
directory is sandboxed. A temporary Linux subreaper runs the full local Go
suite so killed orphan descendants are reaped; no process-tree tests are
excluded. Golangci-lint 2.11.4 uses the official checksum-verified release
binary because building it locally encounters a blocked dependency download.
Remote CI remains responsible for Go 1.23/1.25, native Windows, the clean
interop report and the repository's required `quality` check. Release
acceptance additionally requires recorded human environment approval.
