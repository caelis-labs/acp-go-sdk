# Official SDK interoperability harness

This directory contains the release-gating interoperability matrix for the
stable ACP v1 root package. A Go test runner owns process lifecycle, timeouts,
event recording, and assertions. The TypeScript and Rust programs under
`peers/` are deliberately thin adapters over the pinned official SDKs; they do
not reimplement JSON-RPC or ACP framing.

The core matrix runs both roles in both languages:

- Go client to official TypeScript agent;
- official TypeScript client to Go agent;
- Go client to official Rust agent;
- official Rust client to Go agent.

Each direction covers the deterministic `core`, `session-cancel`, and
`request-cancel` scenarios. `session-cancel` proves prompt-turn cancellation.
`request-cancel` separately proves `$/cancel_request` and JSON-RPC `-32800`.

External SDK identities are recorded in `versions.json` and mirrored by
`upstream/lock.json`. Published dependency artifacts are locked by
`package-lock.json` and `Cargo.lock`; the Rust compiler is selected by the
peer-local `rust-toolchain.toml`.

Run the complete matrix with:

```sh
make interop
```

Ordinary `go test ./...` compiles the Go harness but skips external processes
unless `ACP_INTEROP=1` is set. Peer agents reserve stdout exclusively for ACP
NDJSON. Diagnostics use stderr and machine-readable client observations are
written to runner-provided temporary result files. A successful matrix also
writes `.artifacts/interop/evidence.json` (or `ACP_INTEROP_EVIDENCE_DIR` when
set), recording toolchain/schema identities and the asserted event trace for
all 12 cases. CI uploads that report together with the dependency lock files.
