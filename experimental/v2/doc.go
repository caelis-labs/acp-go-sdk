// Package v2 is an experimental draft ACP protocol v2 surface.
//
// It is generated from a pinned schema-v2* tag and is intentionally isolated
// from the stable root package. The API is not covered by the v1 compatibility
// promise and may change when the official v2 schema changes.
//
// session/prompt returns the required messageId of the inserted user message.
// The matching user-message update may arrive before or after the response.
// Running, idle, requires_action, and stopReason are reported through session/update
// state_update notifications. session/update values with an ID are upserts.
package v2
