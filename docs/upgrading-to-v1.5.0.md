# Upgrading to v1.5.0

The stable root package now tracks official `schema-v1.25.0`. The negotiated
wire protocol remains ACP v1, and existing stable Go API types and methods are
retained. The new stable functionality warrants a minor SDK release. Release
Please updates the version and installation command in the separate Release PR.

## Notices and compaction

Clients opt in through `InitializeRequest.ClientCapabilities.Session`:

```go
sessionCapabilities := &acp.ClientSessionCapabilities{
    Notices:    &acp.NoticeCapabilities{},
    Compaction: &acp.CompactionCapabilities{},
}
```

Omission or `null` means unsupported; `{}` advertises support. Malformed values
for these new capabilities recover to unsupported. Agents must send notices
and compaction updates only when the corresponding capability was advertised.
Application code owns that negotiation and presentation policy.

`SessionUpdate` adds `Notice`, `CompactionUpdate`, and
`CompactionSummaryChunk`. Notices are live information, outside session
history. Compaction updates are upserts keyed by `CompactionId`; summary chunks
append in notification receive order. `NoticeSeverity` and `CompactionStatus`
are open string enums, so custom and future values remain representable.
A notice requires a non-empty title.

Both standalone `CompactionUpdate` and `SessionCompactionUpdate` preserve
three states for `summary`, `error`, and `_meta`:

| State | Meaning | Go operation |
|---|---|---|
| Omitted | Leave the stored value unchanged | `UnsetSummary`, `UnsetError`, `UnsetMeta` |
| `null` | Clear the stored value | `ClearSummary`, `ClearError`, `ClearMeta` |
| Concrete value | Replace the stored value | `SetSummary`, `SetError`, `SetMeta` |

Inspect decoded values with `SummaryState`, `ErrorState`, and `MetaState`.
`SetSummary([]acp.ContentBlock{})` sends an empty array, clearing the retained
summary without losing the concrete-value state. `_meta` retains raw JSON
numbers. Malformed optional compaction patches recover to omitted, leaving
the stored value unchanged; only a genuine JSON `null` clears it. Invalid
summary entries are skipped, preserving valid entries in order.
Required fields and malformed known content variants still fail validation.

## Action input validation

Malformed non-null terminal arguments, environment values, working directories,
output byte limits, file read bounds, and MCP server configurations are rejected
before calling the application handler. RPC requests report invalid params
(`-32602`). Null list/map fields still mean empty, as in the official receivers;
null elements inside action lists are rejected rather than converted to empty
strings or zero-valued objects. An invalid MCP server rejects the entire session
setup instead of silently removing that server.

Malformed terminal authentication methods are dropped from `authMethods`, while
valid sibling methods remain available. A malformed known terminal method cannot
fall back to agent authentication. Existing display-field recovery outside these
released changes is unchanged.

## Experimental v2

`experimental/v2` now tracks `schema-v2.0.0-alpha.8`; its API remains isolated
from the stable root and may change incompatibly. `IdleStateUpdate` is a union
with known, future, and absent stop-reason variants. The old experimental
`StopReason` type/constants are removed by the new schema; pass a string such as
`"end_turn"` to `IdleUpdate`, or use `IdleErrorUpdate` to include JSON-RPC failure
details after prompt insertion. Earlier prompt failures remain error responses.
`SessionStateUpdate.StateUpdate` retains the nested `StateUpdate` union, and
`StateUpdateIdle.IdleStateUpdate` retains the stop-reason union. Inspect their
selected variant (for example, `.Idle.IdleStateUpdate.Error.Error` for failure
details), rather than flattened `State` / `StopReason` / `Error` fields.
This preserves custom and future state/stop-reason payloads through actual
`session/update` notifications, including extension fields and raw JSON numbers.
The `RunningUpdate`, `IdleUpdate`, `IdleErrorUpdate`, and `RequiresActionUpdate`
helpers construct these nested variants. Unknown v2 MCP transports retain their
raw `Other` variant.

The TypeScript 1.8.0 / Rust 3.3.0 four-direction matrix covers stable ACP v1,
including capability negotiation, notices, ordered compaction updates/chunks,
empty summary replacement, explicit error/metadata clear, reverse permission
requests, session cancellation, and request cancellation. V2 changes have Go
loopback and decoding coverage; the official interop matrix remains stable v1.
