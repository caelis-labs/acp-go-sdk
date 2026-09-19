package v2

import (
	"encoding/json"
	"testing"
)

func TestPromptResponseRequiresMessageID(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{}`, `null`, `{"messageId":null}`, `{"messageId":42}`} {
		var response PromptResponse
		if err := json.Unmarshal([]byte(input), &response); err == nil {
			t.Fatalf("accepted invalid prompt response: %s", input)
		}
	}
	input := `{"_meta":{"sequence":9007199254740993},"messageId":"message-1"}`
	var response PromptResponse
	if err := json.Unmarshal([]byte(input), &response); err != nil {
		t.Fatal(err)
	}
	if response.MessageId != "message-1" {
		t.Fatalf("message ID = %q", response.MessageId)
	}
	encoded, err := json.Marshal(response)
	if err != nil || string(encoded) != input {
		t.Fatalf("round trip = %s, %v", encoded, err)
	}
}

func TestToolCallNamePatchSemantics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		field string
		state NullableFieldState
	}{
		{"absent", "", NullableFieldAbsent},
		{"null", `,"name":null`, NullableFieldNull},
		{"value", `,"name":"read_file"`, NullableFieldValue},
		{"empty", `,"name":""`, NullableFieldValue},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := `{"toolCallId":"call-1"` + test.field + `}`
			var call ToolCallUpdate
			call.SetName("stale")
			if err := json.Unmarshal([]byte(input), &call); err != nil {
				t.Fatal(err)
			}
			if call.NameState() != test.state {
				t.Fatalf("name state = %v, want %v", call.NameState(), test.state)
			}
			assertNameRoundTrip(t, call, test.field)

			var update SessionUpdate
			if err := json.Unmarshal([]byte(`{"sessionUpdate":"tool_call_update","toolCallId":"call-1"`+test.field+`}`), &update); err != nil {
				t.Fatal(err)
			}
			if update.ToolCallUpdate == nil || update.ToolCallUpdate.NameState() != test.state {
				t.Fatalf("session update lost name state: %+v", update)
			}
			assertNameRoundTrip(t, update, test.field)

			var permission ToolCallPermissionSubject
			if err := json.Unmarshal([]byte(`{"toolCall":`+input+`}`), &permission); err != nil {
				t.Fatal(err)
			}
			if permission.ToolCall.NameState() != test.state {
				t.Fatalf("permission lost name state: %+v", permission)
			}
			assertNameRoundTrip(t, permission.ToolCall, test.field)
		})
	}
}

func TestToolCallNamePatchMutators(t *testing.T) {
	t.Parallel()
	for _, value := range []interface {
		SetName(string)
		ClearName()
		UnsetName()
		NameState() NullableFieldState
	}{&ToolCallUpdate{ToolCallId: "call-1"}, &SessionToolCallUpdate{ToolCallId: "call-1", SessionUpdate: "tool_call_update"}} {
		value.SetName("read_file")
		assertNameRoundTrip(t, value, `,"name":"read_file"`)
		value.ClearName()
		if value.NameState() != NullableFieldNull {
			t.Fatalf("clear state = %v", value.NameState())
		}
		assertNameRoundTrip(t, value, `,"name":null`)
		value.UnsetName()
		if value.NameState() != NullableFieldAbsent {
			t.Fatalf("unset state = %v", value.NameState())
		}
		assertNameRoundTrip(t, value, "")
	}
}

func assertNameRoundTrip(t *testing.T, value any, field string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	want := ""
	if field != "" {
		want = field[len(`,"name":`):]
	}
	if got := string(object["name"]); got != want {
		t.Fatalf("name = %s, want %s in %s", got, want, encoded)
	}
}
