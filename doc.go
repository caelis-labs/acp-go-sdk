// Package acp provides a Go SDK for building Agent Client Protocol (ACP)
// agents and clients.
//
// Connect an agent to ACP clients, or add ACP agent support to an editor,
// terminal, or application. This community SDK is maintained by Caelis Labs
// and can be used independently of Caelis or any particular agent framework.
//
// # Getting started
//
// Implement [Agent] to serve initialization, session creation, prompts, and
// cancellation. Implement [Client] to receive streamed session updates and
// handle permission requests. Optional filesystem, terminal, and other
// capabilities have separate interfaces; advertise only those you implement.
//
// The transport/stdio package binds an agent to process stdin/stdout or starts
// an agent executable for a client. It drains child stderr and provides
// context-controlled shutdown with owned process-tree cleanup. Runnable agent
// and client examples are linked from the [README].
//
// # Compatibility and validation
//
// The stable root implements ACP wire protocol v1. Wire types and typed
// dispatch are generated from the pinned official schema identified by
// [SchemaTag] and [SchemaCommit]. Go module versions, schema versions, and
// the negotiated wire protocol version are separate identities.
//
// A four-direction interoperability matrix checks Go clients and agents
// against pinned official TypeScript and Rust SDK peers. The release process
// also checks reproducible generation, race/static analysis, fuzzing, fresh
// consumer builds, and native Windows stdio behavior.
//
// [Connection] supplies bounded, concurrent bidirectional JSON-RPC 2.0 over
// newline-delimited JSON, ordered notifications, cancellation, and lossless
// batch frames. [PreparedRequest] supports separate dispatch and response
// contexts, conservative submission-state reporting, and response observation.
//
// Applications retain ownership of model execution, persistence, and permission
// policy. Draft ACP v2 lives separately in experimental/v2 and is not covered
// by the stable v1 API or its official SDK interoperability matrix.
//
// [README]: https://github.com/caelis-labs/acp-go-sdk#run-an-agent-and-client
package acp
