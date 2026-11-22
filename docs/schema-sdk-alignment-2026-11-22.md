# Schema and SDK alignment, 2026-11-22

This update addresses [upstream drift #48](https://github.com/caelis-labs/acp-go-sdk/issues/48).

| Source | Previous pin | New pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.33.0 | [schema-v1.34.1](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.34.1) | `17747b91cd79442e796204b85cad7112bcc876b0` |
| Experimental v2 schema | schema-v2.0.0-alpha.17 | [schema-v2.0.0-beta.3](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-beta.3) | `94824f11ec78f7a6699323a9342e47fa5edf2530` |
| TypeScript SDK | 2.8.0 | [2.9.1](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.9.1) | `ab16fb07b4e7e5dd8e55633b42930857223c4cd0` |
| Rust SDK | 5.4.0 | [5.5.1](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v5.5.1) | `34aea54f7314b42563b09112fe40b0cd5eb29b8a` |

The schema locks record the annotated tag objects, their peeled commits and the
SHA256 for each official release asset. Both `meta.json` files are byte-identical
to the previous pins. The npm registry integrity and Cargo registry checksum are
recorded in the dependency locks. Cargo also pins the companion derive crate to
5.5.1 and the schema crate to 1.21.2. The Rust toolchain remains 1.88.0.

## Stable v1

- Clients opt in to identified plans with `ClientCapabilities.Plan`. The
  `plan_update` notification replaces a plan by ID with structured entries or
  Markdown. `plan_removed` removes it. The legacy `plan` notification remains
  available.
- Clients opt in to exposed child sessions with `ClientCapabilities.Subagents`.
  The schema adds child announcements, child state and cancellation capabilities,
  inter-session messages and chunks, and tool-call content that references a child
  session. Omitted and null capability advertisements both mean unsupported.
  Child-session and message patches retain explicit clears and raw metadata.
- The negotiated ACP major remains 1. The names and directions of existing RPC
  methods are unchanged. Child-session behavior and sending capability-gated
  notifications are responsibilities of the agent and client.

## Experimental v2

- Plans accept typed Markdown and can be removed by ID. Unrecognized plan content
  variants retain their raw JSON.
- `CompactionUpdate.Status` and `SessionCompactionUpdate.Status` now use
  `*CompactionStatus`. Use `SetStatus` to assign a value, `ClearStatus` to send
  an explicit reset to `in_progress`, and `UnsetStatus` to leave it unchanged.
- The typed decoder requires nonempty system-prompt content and validates
  the forbidden fields on elicitation scopes and titled multi-select items.
  The typed `Error` payload requires version data for code `-32022`.
- The official v2 schema now describes method- and direction-specific envelopes.
  The Go SDK keeps method-specific dispatch and uses the bounded JSON-RPC
  connection for framing. The transport retains its separate generic error API.
  Many remaining generated changes are upstream descriptions.

The [interop matrix](../interop/README.md) exercises both wire profiles over
stdio. Its updated cases check plan replacement and removal, the v1 child
announcement, and v2 compaction updates with omitted status and an explicit reset.
