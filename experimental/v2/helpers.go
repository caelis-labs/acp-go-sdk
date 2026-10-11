package v2

import "encoding/json"

// TextBlock constructs a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Text: &ContentBlockText{
		Text: text,
		Type: "text",
	}}
}

// RunningUpdate is a session/update state_update with state=running.
func RunningUpdate() SessionUpdate {
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		StateUpdate:   StateUpdate{Running: &StateUpdateRunning{State: "running"}},
	}}
}

// IdleUpdate is a session/update state_update with state=idle.
func IdleUpdate(reason string) SessionUpdate {
	var idle IdleStateUpdate
	switch reason {
	case "end_turn":
		idle.EndTurn = &IdleStateUpdateEndTurn{StopReason: reason}
	case "max_tokens":
		idle.MaxTokens = &IdleStateUpdateMaxTokens{StopReason: reason}
	case "max_turn_requests":
		idle.MaxTurnRequests = &IdleStateUpdateMaxTurnRequests{StopReason: reason}
	case "refusal":
		idle.Refusal = &IdleStateUpdateRefusal{StopReason: reason}
	case "cancelled":
		idle.Cancelled = &IdleStateUpdateCancelled{StopReason: reason}
	case "error":
		idle.Error = &IdleStateUpdateError{StopReason: reason}
	default:
		// A string-only object cannot fail JSON encoding. Preserve custom and
		// future reasons as the schema's raw Other variant.
		encoded, _ := json.Marshal(map[string]string{"stopReason": reason})
		raw := IdleStateUpdateOther(encoded)
		idle.Other = &raw
	}
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		StateUpdate:   StateUpdate{Idle: &StateUpdateIdle{State: "idle", IdleStateUpdate: idle}},
	}}
}

// IdleErrorUpdate reports failure after prompt insertion. Earlier failures
// remain JSON-RPC errors returned from session/prompt.
func IdleErrorUpdate(failure *Error) SessionUpdate {
	update := IdleUpdate("error")
	update.StateUpdate.StateUpdate.Idle.IdleStateUpdate.Error.Error = failure
	return update
}

// RequiresActionUpdate is a session/update state_update with state=requires_action.
func RequiresActionUpdate() SessionUpdate {
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		StateUpdate:   StateUpdate{RequiresAction: &StateUpdateRequiresAction{State: "requires_action"}},
	}}
}
