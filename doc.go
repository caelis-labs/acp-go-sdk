// Package acp implements stable Agent Client Protocol v1.
//
// Wire types and typed dispatch are generated from the immutable official
// schema release identified by SchemaTag and SchemaCommit. Connection owns a
// bounded, concurrent JSON-RPC 2.0 session over newline-delimited JSON.
// Draft transports and unstable protocol surfaces are intentionally absent
// from the stable root package.
package acp
