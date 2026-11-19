# Schema and SDK alignment, 2026-11-19

This update addresses [upstream drift #45](https://github.com/caelis-labs/acp-go-sdk/issues/45).
It uses the latest compatible official releases available on 2026-11-19.

| Source | Previous pin | New pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.31.0 | [schema-v1.33.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.33.0) | `50672b4ce6563a90dd0e4a82a2d3a398dc1181ee` |
| Draft v2 schema | schema-v2.0.0-alpha.15 | [schema-v2.0.0-alpha.17](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-alpha.17) | `50672b4ce6563a90dd0e4a82a2d3a398dc1181ee` |
| TypeScript SDK | 2.6.0 | [2.8.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.8.0) | `97d748e705a03be67ce4cb4c78ff0011fcca7592` |
| Rust SDK | 5.2.0 | [5.4.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v5.4.0) | `0fe20235100691d2aa1bc38f729e8ab156338b4f` |

The pinned `schema.json` and `meta.json` files are official release assets. The
locks record the annotated schema tag objects, their peeled commits and the
asset SHA256 digests. The npm package's `gitHead` and the Rust crate's VCS commit
match the SDK tags. The npm integrity and Cargo checksums are locked. Cargo
updates the ACP library and derive crate to 5.4.0 and the schema crate to
1.19.0. Rust stays pinned to 1.88.0.

## Wire and Go API

The v1 assets are byte-identical to schema-v1.31.0. The stable wire version and
generated API are unchanged. The stable package updates its schema provenance
constants. Upstream's separate unstable assets include v1 subagent and session
cursor changes. Those assets are outside the repository's stable policy.

The draft changes are confined to `experimental/v2`:

- `session/prompt` accepts an optional case-sensitive `idempotencyKey`.
  The Go decoder accepts 1–255 Unicode characters and treats an omitted or
  null key as absent. Agents own the session-scoped record of insertions and
  return the previous message ID on a retry.
- A successful cursor-based `session/resume` emits `replay_complete` after
  replay and before the response. Agents send the marker even for empty
  replay. Omitted and null cursors request no replay.
- New typed variants carry subagent announcements, inter-session message
  upserts and chunks, display references, and background subagent items.
  The new `unknown` state represents unconfirmed activity. Ownership, message
  delivery, and applying child-session controls belong to the agent and client.
- A diff requires at least one change. After dropping invalid items, the
  decoder rejects a diff with no valid changes. `StringPropertySchema.Pattern`
  is removed in the draft API. Applications using it must update when moving
  to alpha.17.
- The specification now requires agents to cancel outstanding permission
  requests when abandoning foreground work. It also clarifies tool call
  status transitions and how clients display unknown command kinds.

## Verification scope

The [interop matrix](../interop/README.md) runs both wire profiles against the
published TypeScript and Rust SDKs over stdio. It retains the v1
new/load/resume and v2 new/resume system-prompt tests, including override,
and verifies that v2 rejects `session/load`. V2 now exercises a keyed retry,
the replay boundary marker, and the new notification and snapshot variants.
The fixtures do not cover child-session controls or a pending permission
request during foreground cancellation. The broader matrix boundaries are
documented alongside the tests.
