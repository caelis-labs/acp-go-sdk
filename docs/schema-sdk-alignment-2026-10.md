# October 2026 schema and SDK alignment

This change addresses [upstream drift #25](https://github.com/caelis-labs/acp-go-sdk/issues/25).
It does not change the Go module release version or the stable ACP wire major (1).

| Source | Previous pin | Updated pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.25.0 | [schema-v1.26.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.26.0) | e2d83349bc9e28a4059ea3acefa1c91c966f74e2 |
| Draft v2 schema | schema-v2.0.0-alpha.8 | [schema-v2.0.0-alpha.9](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.9) | e2d83349bc9e28a4059ea3acefa1c91c966f74e2 |
| TypeScript SDK | 1.8.0 | [1.9.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v1.9.0) | 1db4d43286d993545e729376346b7f9098748159 |
| Rust SDK | 3.3.0 | [3.4.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v3.4.0) | 4f82aef0e7559a50c2b95aff4f7ba60af10d9e04 |

The schema locks contain exact tag objects and downloaded asset SHA256 values.
Interop locks contain the npm integrity and crates.io checksum. The npm
`gitHead` and the Rust crate's `.cargo_vcs_info.json` match the SDK tag commits.
Rust's dependency update also selects schema crate 1.12.0 and derive crate 3.4.0.

## Stable compatibility

The stable `schema.json` and `meta.json` assets are byte-identical to 1.25.0.
Their SHA256 values remain `4bcd1d2051ff7ed379bd9b234fb9b09c5ab9245478698e9fab805bd9b52a33a6`
and `061edb6efa8fb2aa2792459a86ec7268de5fe665bba48b2ffe7939df01481f88`.
Stable wire types, constants and helpers regenerate without changes. Stable
public declarations change only the three schema provenance constant values.
Client system prompts and the removal of upstream next-edit suggestions affect
unstable schemas; they do not introduce stable Go interfaces here.

Typed request dispatch now interprets omitted or null params as `{}`, matching
the official SDKs' empty-payload handling. This enables optional-only requests
such as `session/list` and `logout`. Required fields remain mandatory: an empty
initialize, new-session or prompt request returns `-32602` before the handler.
Invalid scalar and array payloads remain errors. This normalization does not
change raw `Connection` handlers or notification dispatch.

Subprocess stdout also remains readable after `Process.Wait` until explicit
`Close` or `Shutdown`. The internal waiter owns process reaping without closing
the application's read end, so a fast peer's final protocol frames are retained.

## Isolated experimental v2 changes

The alpha.9 schema removes `file` and `markdown` from the exclusion list of the
`PlanUpdateContent` catch-all variant. Both can now pass through as raw unknown
plan content. The existing generated representation already preserves these
objects, so no public Go type removal is needed. No v2 definitions are added
to the stable root package.

Entity upserts now retain omitted/null/value for every optional nullable patch
field, including message content, tool calls, terminals and metadata. Generated
`State`, `Set`, `Clear` and `Unset` helpers preserve explicit empty arrays and
objects. Malformed recoverable patches become omitted and must not clear stored
values. Metadata values retain exact JSON numbers and null-valued keys.

For upsert `_meta`, omission leaves stored metadata unchanged, top-level null
clears it, and an object patches by top-level key. Omitted keys stay unchanged,
null-valued keys are removed, and other values replace the whole key without a
nested merge. `{}` is a no-op. Chunk/content/snapshot/plan metadata has its own
schema meaning. The SDK transports patches; applications own stored state and
must apply these rules. Stable compaction metadata retains its stable semantics.

V2 agents select protocol 2 when a peer requests 2 or a higher valid major.
The router passes every initialize field through when selecting v2, rewriting
only the protocol version when needed. This preserves unknown capabilities,
implementation extensions, `_meta`, and exact numeric payloads for downstream
handlers. A v1-only router retains the legacy fallback to v1; converting a v2
initialize carries only shared identity/auth/elicitation/metadata fields and
never enables v1 filesystem or terminal I/O from unknown v2 capabilities.
The typed v2 client rejects a response selecting a major other than 2. Repeated
initialization and routing before successful initialization remain rejected.

Prompt success acknowledges insertion and returns the user message ID. The
matching user-message update can arrive before or after that response. Neither
the response nor a message upsert/chunk completes foreground activity. Message
upserts replace content; later chunks append. Foreground activity is reported
separately through running/requires-action/idle state snapshots. Repeated idle
snapshots may repeat the last stop reason and must not be counted as additional
completions or usage. Background work can continue while foreground state is
idle. Applications retain responsibility for execution and lifecycle policy.

## Validation boundary

The official TypeScript 1.9.0 / Rust 3.4.0 matrix exercises both roles in all
four stable-v1 directions, with core, session-cancel and request-cancel cases.
Core includes advertised display capabilities, compaction patches, ordered
updates, reverse permission requests and empty-request probes. Rust's public
transport canonicalizes outgoing null params to omitted, so the Rust-client
probe does not claim to emit an explicit wire null.

V2 tests exercise Go wire roundtrips and actual loopback notifications/requests,
including initialize payload forwarding, version negotiation, patch recovery,
message replacement/chunks and insertion-versus-activity completion. They are
not official-SDK v2 interoperability evidence. Native Windows execution and
remote CI are not part of a local Linux validation claim.
