# Versioned interoperability follow-up, 2026-11-13

This follows #42's schema/SDK alignment. The pinned upstream artifacts remain
schema-v1.31.0, schema-v2.0.0-alpha.15, TypeScript 2.6.0 and Rust 5.2.0. It adds
independent official-SDK v2 coverage and expands the v1 system prompt contract.
The [matrix and its boundaries](../interop/README.md) are the maintained source
for the 89 external cases and three machine-readable evidence reports.

Stable v1 retains the original 12 conversation/cancellation cases and gains 16
system prompt contract cases. V2 gains 40 four-direction contract cases and 21
raw receiver probes. Application validation is implemented independently in Go,
TypeScript and Rust, using the corresponding SDK for wire handling. Shared
immutable JSON fixtures specify requests, expected fields, errors and updates;
the SDK peers do not share the Go application engine.

The new receiver probes found a Go v2 error-envelope mismatch: a request with a
fractional ID returned `-32600` but echoed the invalid ID. Official TypeScript
and Rust agents use a null error ID. `ConnectionOptions.AcceptRequestID` is an
additive, opt-in admission hook that rejects an ID before any handler executes.
Typed v2 connections install `v2.IsValidRequestID`; raw v2 routers must opt in.
Stable root defaults are unchanged. Tests cover both typed v2 sides, invalid
IDs, large signed integer precision and preservation of the stable fractional-ID
behavior. The codec round-trip gate also found that v2 tool upserts dropped explicit
null rawInput/rawOutput values. The generator now tracks presence for those
unconstrained JSON fields on metadata-patch objects, with Set/Clear/Unset
accessors and marshal-error propagation. Only v2 generated code changes;
stable v1 generated code and upstream locks remain unchanged. The same
round-trip gate caught empty background snapshots being omitted: `[]` reports
no background work, while omission/null means unreported. Generated v2
state encoders now preserve empty snapshots and keep null equivalent to absence.

The matrix tests omitted capabilities as unsupported and keeps stable/draft
surfaces isolated. V2 has no session/load. Omitted/null system prompts on
load/resume may retain prior instructions or use defaults; the SDK does not
choose that application policy. Override does not change fixture permission
policy or MCP configuration. Pending request cancellation is distinguished from
foreground session cancellation and per-entity cancellation, including a remote
acknowledgement before checking side effects.

The v2 matrix covers every agent method and client method in the pinned
non-unstable schema, all known update discriminators, and unknown update
preservation. It includes state updates, background-work snapshots, replay,
message-ID association before/after the prompt response, reverse permission and
form/URL elicitation, configuration and authentication wire behavior. Fixture
commands and URLs are never executed or visited. These checks verify protocol
exchange and application-owned fixture effects, not model/auth/tool behavior.

Validation includes both Go modules' tests, vet, race and generation checks,
golangci-lint, example and fresh consumer builds, API comparison, fuzz replay
and bounded fuzzing, Rust formatting/Clippy/tests, and the complete external
matrix. Cloud process-tree tests use a temporary Linux subreaper without
excluding tests. The consumer disables buildvcs for the sandboxed parent Git
directory; lint uses the checksum-verified official 2.11.4 binary. Remote CI
adds Go 1.23/1.25 and native Windows transport checks. No release workflow is
manually triggered and passing draft v2 interop does not make v2 stable.
