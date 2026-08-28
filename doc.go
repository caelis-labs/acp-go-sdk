// Package acp provides a product-neutral Go SDK for Agent Client Protocol
// (ACP) v1.
//
// Wire types and typed dispatch are generated from the immutable official
// schema release identified by SchemaTag and SchemaCommit. Connection owns a
// bounded, concurrent JSON-RPC 2.0 session over newline-delimited JSON.
// PreparedRequest provides a product-neutral outbound request lifecycle with
// separate dispatch and response contexts, conservative transport submission
// classification, decode-independent response observation, and explicit local
// abandon.
// Draft transports and unstable protocol surfaces are intentionally absent
// from the stable root package.
package acp
