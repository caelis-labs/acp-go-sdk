package acp

import (
	"bytes"
	"context"
	"encoding/json"
)

// SessionUpdateRaw sends a session/update notification without decoding the
// update union. It is intended for transparent forwarding and compatibility
// across schema revisions. The caller remains responsible for the ACP
// semantics of the opaque update payload.
//
// The outer SessionNotification envelope is still checked, and the method is
// always fixed to session/update. The original params are sent through the
// connection's normal bounded write and notification-ordering path.
func (c *AgentSideConnection) SessionUpdateRaw(ctx context.Context, params json.RawMessage) error {
	if err := validateRawSessionNotification(params); err != nil {
		return err
	}
	return c.conn.SendNotification(ctx, ClientMethodSessionUpdate, params)
}

func validateRawSessionNotification(params json.RawMessage) *RequestError {
	if !json.Valid(params) {
		return NewInvalidParams(map[string]any{"error": "session/update params must be valid JSON"})
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(params, &envelope); err != nil || envelope == nil {
		return NewInvalidParams(map[string]any{"error": "session/update params must be an object"})
	}
	rawSessionID, ok := envelope["sessionId"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawSessionID), []byte("null")) {
		return NewInvalidParams(map[string]any{"error": "session/update sessionId is required"})
	}
	var sessionID string
	if err := json.Unmarshal(rawSessionID, &sessionID); err != nil {
		return NewInvalidParams(map[string]any{"error": "session/update sessionId must be a string"})
	}
	rawUpdate, ok := envelope["update"]
	if !ok || bytes.Equal(bytes.TrimSpace(rawUpdate), []byte("null")) {
		return NewInvalidParams(map[string]any{"error": "session/update update is required"})
	}
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil || update == nil {
		return NewInvalidParams(map[string]any{"error": "session/update update must be an object"})
	}
	return nil
}
