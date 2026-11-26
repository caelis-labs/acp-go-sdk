# Draft ACP v2 (`experimental/v2`)

Draft ACP v2 types and helpers live in the isolated `github.com/caelis-labs/acp-go-sdk/experimental/v2` package.

> [!WARNING]
> `experimental/v2` is generated from a draft upstream schema tag and **is not a stable API**. Types, methods, and wire mappings may change incompatibly between releases. Do not import `experimental/v2` from the stable root `acp` package.

## Pinned identity and validation boundary

<!-- upstream-pins:v2:start -->
| Identity | Pinned value |
|---|---|
| Upstream repository | [`agentclientprotocol/agent-client-protocol`](https://github.com/agentclientprotocol/agent-client-protocol) |
| Schema tag | `schema-v2.0.0-beta.4` |
| Upstream commit | `840484ff59b3deb1314d1a74e351cd16ef052b30` |
| `schema/v2/schema.json` SHA256 | `86dae71cce55d412b927383cb3838ea560bdbf683ea8903bede75785fc79c494` |
<!-- upstream-pins:v2:end -->

- Draft v2 tracks tagged `schema-v2*` snapshots recorded in [`schema/v2/lock.json`](../schema/v2/lock.json) and [`upstream/lock.json`](../upstream/lock.json), never unpinned commits on upstream `main`.
- Generated stable code in the root `acp` package uses [`schema/schema.json`](../schema/schema.json) only; `schema.unstable.json` and v2 schemas are never merged into the stable package.
- `experimental/v2` typed dispatch, decoding, and lifecycle have dedicated Go loopback coverage as well as an independent four-direction official TypeScript/Rust interoperability gate (40 draft v2 contract cases and 21 v2 wire receiver probes, plus opt-in unstable MCP-over-ACP probes; see [`interop/README.md`](../interop/README.md)). Passing draft v2 interop does not make `experimental/v2` a stable API.

## Draft v2 protocol semantics covered

- **`session/prompt` and session state updates:** `session/prompt` returns the required `messageId` of the user message inserted into the conversation; the matching user-message update may arrive before or after that response. `running`, `idle`, `requires_action`, and `unknown` are `session/update` states.
- **Discriminated `AvailableCommands`:** V2 new/resume responses expose optional `AvailableCommands`. Each command is a discriminated union: use its `Command` branch with `type: "command"`, or preserve unknown kinds through the raw `Other` branch. Omission, an empty array, `null`, or a non-array value advertises no initial commands; malformed entries are skipped while valid entries retain their order and command input extensions. Sending an empty list omits the optional field and never emits `null`. Later `available_commands_update` notifications replace the list; an empty array clears it.
- **Tri-state tool-call `name`:** In v2, tool-call updates distinguish omission, explicit `null` (clear), and a string (replace) through generated `NameState`, `SetName`, `ClearName`, and `UnsetName` methods.

## Alignment and migration history

- [Alignment notes (2026-11-24)](schema-sdk-alignment-2026-11-24.md)
- [Alignment notes (2026-11-22)](schema-sdk-alignment-2026-11-22.md)
- [Alignment notes (2026-11-19)](schema-sdk-alignment-2026-11-19.md)
- [Alignment notes (2026-11-13)](schema-sdk-alignment-2026-11-13.md)
- [Versioned interop follow-up (2026-11-13)](versioned-interop-2026-11-13.md)
- [Alignment notes (2026-11-04)](schema-sdk-alignment-2026-11-04.md)
- [Alignment notes (2026-10-29)](schema-sdk-alignment-2026-10-29.md)
- [Alignment notes (2026-10)](schema-sdk-alignment-2026-10.md)
- [v1.5.0 migration notes](upgrading-to-v1.5.0.md)
- [v1.4.0 migration notes](upgrading-to-v1.4.0.md)
