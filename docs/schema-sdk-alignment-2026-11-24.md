# Schema and SDK alignment, 2026-11-24

This update addresses [upstream drift #51](https://github.com/caelis-labs/acp-go-sdk/issues/51).

| Source | Previous pin | New pin | Tagged commit |
| --- | --- | --- | --- |
| Stable schema | schema-v1.34.1 | [schema-v1.35.0](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v1.35.0) | `065b57951b6b2a91ac5eddd249e145d1f6d54545` |
| Draft v2 schema | schema-v2.0.0-beta.3 | [schema-v2.0.0-beta.4](https://github.com/agentclientprotocol/agent-client-protocol/releases/tag/schema-v2.0.0-beta.4) | `840484ff59b3deb1314d1a74e351cd16ef052b30` |
| TypeScript SDK | 2.9.1 | [2.12.0](https://github.com/agentclientprotocol/typescript-sdk/releases/tag/v2.12.0) | `6b84c6da9430a1e4aba9c87c0b45f8b43a6aea9a` |
| Rust SDK | 5.5.1 | [5.6.0](https://github.com/agentclientprotocol/rust-sdk/releases/tag/v5.6.0) | `81e9d7284024cca48e5756615b47f5102fe37e3d` |

Schema locks record the tag objects, peeled commits and SHA256 digests for the
official release assets. npm `gitHead` and the Rust crate's VCS commit match
their tags. Rust stays on 1.88.0 and the schema crate resolves to 1.22.0.

In stable v1, an explicit `type: "boolean"` requires a JSON boolean. The
string-ID branch remains the default when the type is omitted and accepts a
string with an unknown type. Go's existing discriminant-first decoder already
enforces this distinction. The new tests exercise the public Go client and
agent entry points and all four official-SDK directions. The v2 beta.4
non-unstable assets clarify text and Markdown rendering. The v2 generated
wire types retain their shapes and remain in `experimental/v2`. The public API
diff only reports the expected schema provenance constants.

The [experimental MCP-over-ACP binding](https://agentclientprotocol.com/rfds/mcp-over-acp#session-and-tool-call-attribution)
adds optional session and tool attribution to both wire profiles. The
[interop matrix](../interop/README.md) exercises the opt-in TypeScript bridge
and official TypeScript and Rust provider admission APIs against Go test
peers. The tests verify attribution to the announced child, rejection before
execution when the session is missing or disallowed, and correlation with a
tool ID independently of admission. The bridge test drops a request while
it waits for the child announcement, then releases the lookup and checks that
the provider never received the abandoned operation.

These tests use the published SDKs' experimental entrypoints and test-owned
routing. The unstable schemas stay outside the generated Go packages. The
suite covers neither ordinary stdio MCP execution nor a Codex adapter.
Rust SDK 5.6.0 could send an active-parent request to the v1 session update
stream before MCP admission, as reported in
[rust-sdk#528](https://github.com/agentclientprotocol/rust-sdk/issues/528).
The [interop matrix](../interop/README.md) now pins 5.6.1 and checks the
active-parent rejection in both wire profiles after
[rust-sdk#529](https://github.com/agentclientprotocol/rust-sdk/pull/529).
