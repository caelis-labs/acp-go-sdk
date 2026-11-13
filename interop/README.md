# Versioned official SDK interoperability

`make interop` runs the stable v1 suite and an independent draft v2 suite over
real stdio processes. Both profiles run all four directions: Go client to
TypeScript/Rust agent, and TypeScript/Rust client to Go agent. The peers use
published SDKs for JSON-RPC framing, IDs, cancellation and versioned dispatch;
fixture engines implement test application behavior and never execute their
MCP commands, tools or terminal commands.

| Suite | Profile | Directions / receivers | Cases |
| --- | --- | --- | --- |
| Conversation, permission, notices/compaction, empty params, session/request cancellation | v1 | Four directions × three scenarios | 12 |
| System prompt lifecycle and capability contracts | v1 | Four directions × four capability profiles | 16 |
| System prompt lifecycle and capability contracts | v2 | Four directions × four capability profiles | 16 |
| Omitted/null session capability | v2 | Four directions × two profiles | 8 |
| Lifecycle, auth/config, reverse calls, states and cancellation | v2 | Four directions | 4 |
| User-message update after prompt response | v2 | Four directions | 4 |
| Version negotiation / repeated initialize window | v2 | Four directions | 4 |
| Baseline content and unsupported content rejection | v2 | Four directions | 4 |
| Invalid-envelope and unsupported-method wire probes | v2 | Go, TypeScript and Rust agents × seven probes | 21 |
| **Total** | | | **89** |

The four system prompt profiles advertise override, append only, omitted support,
or explicit null support. V1 exercises new/load/resume; v2 exercises new/resume
and explicitly rejects session/load. Tests cover omitted/null prompts, append
and override, missing/null required fields, unknown/custom modes, unsupported
content and empty arrays. Rejections are followed by lifecycle mutation-count
checks. Permission policy and MCP configuration are inspected across successful
and rejected overrides. The fixture agents retain the old prompt for omitted/null
load/resume, but assertions deliberately allow either retention or defaults:
that choice belongs to the application, not the SDK.

The v2 lifecycle case exercises all 13 agent methods in the pinned non-unstable
schema, all four client methods, and every known session-update discriminator.
It checks prompt message IDs against user-message upserts; chunks, repeated IDs,
unknown variants and explicit patch clears; running/requires_action/idle ordering;
reverse permission plus form and URL elicitation and completion; authentication
login/logout; boolean config success and rejection without mutation; resume
replay followed by a current-state snapshot; close/delete/list behavior; and
unsupported replay cursors. State snapshots and tool/terminal cancellation use
agent-owned fixture work, without starting OS processes.

`session/cancel` stops foreground fixture work and preserves background work.
`$/cancel_request` cancels a pending prompt before insertion and returns `-32800`;
a remote acknowledgement is required before checking cancellation side effects.
Per-entity cancellation is only sent for advertised controls, produces completion
updates and clears the controls; repeated/unknown cancellation is rejected.
The `_interop/flush` extension deterministically releases user-message/state
notifications after a prompt response so both legal response/update orderings
are exercised. The extensions `_interop/inspect` and `_interop/flush` exist only
in these test agents and never enter generated ACP objects or the stable API.

TypeScript 2.6.0 uses its default v1 entrypoint and, separately,
`@agentclientprotocol/sdk/experimental/v2`. Rust 5.2.0 explicitly uses `.v1()`
and `.v2()`, with `unstable_protocol_v2` enabled only in the peer dependency.
No additional unstable feature set is enabled. SDK package majors do not choose
the ACP wire major. Go contract peers use the bounded connection API, v2's
protocol router and generated payload decoders; high-level v2 helpers also have
Go loopback coverage. This suite does not claim to test every SDK convenience
wrapper.

Official v2 clients pin outgoing initialize to their configured v2 profile.
The lower-version rejection probe therefore uses raw NDJSON, rather than claim
that passing `protocolVersion: 1` through those clients sends a v1 handshake.
Raw probes also cover requests before initialize, batch arrays, fractional/null
IDs, session/load and unknown methods. Typed v2 connections install
`AcceptRequestID: v2.IsValidRequestID` and reject invalid IDs before dispatch,
replying with a null error ID. Raw v2-only router users must install that
validator and `RejectBatch` themselves; those connection-wide restrictions also
apply to v1 traffic if callers choose to use them on a mixed-profile router.
The stable root default still accepts its existing numeric request ID forms.

## Evidence and CI gate

The expected case names, required v2 methods and update variants are checked in
ordinary Go tests. `ACP_INTEROP=1` enables external processes; ordinary Go tests
otherwise skip them. A partial external run cannot claim a complete matrix.
Each client emits a per-step trace, which the Go runner checks against the
entire case. Arrays, scalar values and explicit null are compared exactly;
object expectations select the fields relevant to each assertion.

`make interop` additionally requires TypeScript compilation, Rust formatting,
Clippy and tests. The existing `official-sdk-interop` CI job runs this complete
suite and `quality` requires its success. CI uploads the dependency locks and
all reports under `.artifacts/interop/`:

- `evidence.json`: the original 12 stable v1 conversation/cancellation cases;
- `contract-evidence.json`: all 56 versioned contract cases, fixture SHA256s,
  schema/SDK identities, toolchains, head commit and per-step/reverse-call traces;
- `wire-v2-evidence.json`: all 21 receiver probes and observed error envelopes.

All reports distinguish complete success from failure, and record commit and
working-tree state. Use `ACP_INTEROP_EVIDENCE_DIR` to change their destination.
Exact package identities remain in `versions.json`, `upstream/lock.json`, npm's
package lock and Cargo.lock; Rust stays pinned to 1.88.0.

## Verification boundaries

“Complete” means every cell and assertion in the table above, not a proof of
all protocol permutations or all third-party agents. The matrix uses Linux
stdio and bounded deterministic test applications. Native Windows Go stdio
has a separate CI job; external SDK matrices are not run on Windows. The suite
does not execute models, OAuth/browser login, MCP servers, actual tool commands,
real terminals, HTTP/SSE/WebSocket transports or additional unstable extensions.
It does not claim all optional v1 filesystem/terminal/mode methods, every content
modality, arbitrary unsafe JavaScript numeric IDs, or every recovery/concurrency
schedule. Go race, fuzz, transport and loopback tests supplement these external
cases. Draft v2 remains experimental regardless of passing interoperability.
