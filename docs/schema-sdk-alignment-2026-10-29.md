# Schema and SDK alignment, 2026-10-29

This change addresses [upstream drift #36](https://github.com/caelis-labs/acp-go-sdk/issues/36).
It keeps the stable wire major at 1 and does not change the module release version.

| Source | Previous pin | Updated pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.26.0 | [schema-v1.27.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.27.0) | 97fcb0f5b88567dd91d585ea6885761365a6aa7b |
| Draft v2 | schema-v2.0.0-alpha.9 | [schema-v2.0.0-alpha.10](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.10) | 97fcb0f5b88567dd91d585ea6885761365a6aa7b |
| TypeScript SDK | 2.0.2 | [2.1.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.1.0) | dbbe157eec813ab64b5199d6660540e5da513f22 |
| Rust SDK | 3.5.0 | [3.6.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v3.6.0) | a978f01e8cde58a7976666a713531d8c9342fb51 |

The schema files are the exact release assets, with tag objects, commits and
SHA256 hashes in their respective locks. Meta assets are unchanged. The npm
`gitHead` and the Rust crate's `.cargo_vcs_info.json` match the SDK tag commits.
The npm integrity and crates.io checksum are mirrored by the interop lock and
dependency assertions. Cargo updates only the SDK/derive crates to 3.6.0 and
schema crate to 1.13.0. Rust remains pinned to 1.88.0; Zod remains at 4.6.7.

## Stable v1

The [tagged schema comparison](https://github.com/agentclientprotocol/agent-client-protocol/compare/schema-v1.26.0...schema-v1.27.0)
updates documentation links and adds `x-deserialize-default-on-null` to action
lists/maps. The generated receiver now reads that hint instead of inferring it
from field names. Explicit null still means empty for those fields; malformed
non-null values and null list elements still fail. The generated wire
structures and stable public declarations remain unchanged apart from schema
provenance constant values. No v1 unstable or v2 definitions enter the root.

The [TypeScript comparison](https://github.com/agentclientprotocol/typescript-sdk/compare/v2.0.2...v2.1.0)
adds unstable MCP-over-ACP helpers, schema parsing alignment, privacy fixes and
v2 negotiation changes. Our peer keeps the default stable ACP v1 entrypoint
and the same `agent()` / `client()` stdio APIs. The
[Rust comparison](https://github.com/agentclientprotocol/rust-sdk/compare/v3.5.0...v3.6.0)
updates its schema dependency, v2 negotiation, examples and logging redaction.
The peer requires no API migration. These SDK updates do not add MCP-over-ACP
or HTTP/SSE APIs to this SDK.

## Experimental v2

Alpha.10 changes the experimental API and receiver semantics:

- `AvailableCommand` is a discriminated union. Use
  `AvailableCommand{Command: &AvailableCommandCommand{Type: "command", ...}}`.
  Unknown types retain their raw payload. `AuthCapabilities` becomes
  `ClientAuthCapabilities`.
- Request IDs must be strings or signed 64-bit integers. Null IDs are rejected
  at both typed connection boundaries, including unknown extension methods.
  The stable connection keeps its existing ID behavior.
- Initialization inspects the version first. No common supported version
  produces `-32022` with `requested` and non-null `supported` fields. Newer
  versions select 2 and discard fields that cannot be parsed in that profile;
  an unusable implementation identity becomes `unknown`/`unknown`. Valid raw
  extensions remain intact. A v1-only router keeps the legacy v1 fallback.
- V2 initialization can repeat before any other request, keeping the selected
  version. Concurrent attempts, attempts after a failed first handshake and
  repeats after another request are rejected. Notifications do not end the
  repeat window. A failed repeat retains the previous selection.
- Elicitation requires exactly one valid session/request scope, including
  unknown modes. The form-schema root defaults to `object` only when omitted;
  an explicit other type or null fails. Scope constructor helpers carry their
  shared form/URL fields.
- The generator follows v2 recovery hints throughout the isolated surface.
  Optional malformed fields recover to omission. Replacement command/config/
  plan lists reject malformed non-array values, skip invalid entries inside
  arrays and preserve an explicit empty array. Invalid patches cannot clear
  retained values. Listed session directories and `nextCursor` parse strictly;
  null directories still mean no roots, as specified by the tagged description
  (the published schema lacks the null hint on this particular field).
- Unknown variants preserve optional fields verbatim, including malformed
  optional `_meta`, while retaining their required-field checks.
- Terminal upserts include optional nullable `size` with nonzero uint16 rows
  and columns. Its generated presence helpers preserve omission/null/value.
  `exitCode` is signed int64, `ResourceLink.size` is uint64, and a tool-call
  location's line is 1-based. Newly added `_meta` on diff patches, icons,
  permission subjects and cancelled outcomes preserves exact JSON numbers.

These are wire/dispatch responsibilities. Applications still own retained
state, execution policy, capability policy and rendering decisions.

## Validation and evidence

Run `make check test-race`, example builds, the fresh consumer module smoke,
fuzz corpus replay and the pinned public API comparison. The Go regressions
cover alpha.10 parsing, IDs across actual connections, version-first errors,
repeat initialization and existing lifecycle/patch behavior.

`make interop` builds the exact npm and Cargo artifacts, checks Rust formatting,
Clippy and tests, then runs all 12 stable-v1 cases across the four directions.
It writes `.artifacts/interop/evidence.json`; CI uploads it with the dependency
locks. This remains stable-v1 stdio evidence. Experimental v2 has Go wire and
loopback coverage, without a claim of official-SDK v2 interoperability.

The cloud Linux workspace uses `GOFLAGS=-buildvcs=false` because its parent Git
directory is sandboxed. Go 1.23/1.25 and native Windows stdio are verified by
the existing full remote CI workflow. Release-only approval/acceptance jobs are
not applicable to this implementation PR.

Local validation excludes `TestProcessShutdownTerminatesOwnedProcessTree` and
`TestStartContextCancellationTerminatesOwnedProcessTree`: this container's PID 1
does not reap orphaned grandchildren, so process-group disappearance never
completes. The unchanged main snapshot reproduces the first test's timeout at
the same `unixProcessTree.terminate` wait. No stdio code or CI exclusions were
changed; full process-tree coverage remains required in remote CI.
