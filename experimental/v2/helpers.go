package v2

// TextBlock constructs a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Text: &ContentBlockText{
		Text: text,
		Type: "text",
	}}
}

// RunningUpdate is a session/update state_update with state=running.
func RunningUpdate() SessionUpdate {
	state := "running"
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		State:         &state,
	}}
}

// IdleUpdate is a session/update state_update with state=idle.
func IdleUpdate(reason StopReason) SessionUpdate {
	state := "idle"
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		State:         &state,
		StopReason:    &reason,
	}}
}

// RequiresActionUpdate is a session/update state_update with state=requires_action.
func RequiresActionUpdate() SessionUpdate {
	state := "requires_action"
	return SessionUpdate{StateUpdate: &SessionStateUpdate{
		SessionUpdate: "state_update",
		State:         &state,
	}}
}
