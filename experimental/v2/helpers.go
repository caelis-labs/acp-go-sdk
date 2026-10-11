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
		StateUpdate: StateUpdate{Running: &StateUpdateRunning{}},
	}}
}

// IdleUpdate is a session/update state_update with state=idle.
func IdleUpdate(reason string) SessionUpdate {
	var idle IdleStateUpdate
	switch reason {
	case "end_turn":
		idle.EndTurn = &IdleStateUpdateEndTurn{}
	case "max_tokens":
		idle.MaxTokens = &IdleStateUpdateMaxTokens{}
	case "max_turn_requests":
		idle.MaxTurnRequests = &IdleStateUpdateMaxTurnRequests{}
	case "refusal":
		idle.Refusal = &IdleStateUpdateRefusal{}
	case "cancelled":
		idle.Cancelled = &IdleStateUpdateCancelled{}
	case "error":
		idle.Error = &IdleStateUpdateError{}
	default:
		// A string-only object cannot fail JSON encoding. Preserve custom and
		// future reasons as the schema's raw Other variant.
		encoded, _ := json.Marshal(map[string]string{"stopReason": reason})
		raw := IdleStateUpdateOther(encoded)
		idle.Other = &raw
	}
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		StateUpdate: StateUpdate{Idle: &StateUpdateIdle{IdleStateUpdate: idle}},
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
		StateUpdate: StateUpdate{RequiresAction: &StateUpdateRequiresAction{}},
	}}
}
