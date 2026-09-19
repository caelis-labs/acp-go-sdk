package acp

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestToolCallNameWireShapes(t *testing.T) {
	t.Parallel()
	for _, shape := range []struct {
		name string
		wire string
		new  func() any
	}{
		{"call", `{"toolCallId":"call-1","title":"Read","status":"pending"%s}`, func() any { return new(ToolCall) }},
		{"update", `{"toolCallId":"call-1"%s}`, func() any { return new(ToolCallUpdate) }},
		{"session-call", `{"sessionUpdate":"tool_call","toolCallId":"call-1","title":"Read","status":"pending"%s}`, func() any { return new(SessionUpdate) }},
		{"session-update", `{"sessionUpdate":"tool_call_update","toolCallId":"call-1"%s}`, func() any { return new(SessionUpdate) }},
		{"permission", `{"sessionId":"s","options":[],"toolCall":{"toolCallId":"call-1"%s}}`, func() any { return new(RequestPermissionRequest) }},
	} {
		for _, field := range []string{"", `,"name":null`, `,"name":"read_file"`, `,"name":""`} {
			t.Run(shape.name+"/"+field, func(t *testing.T) {
				value := shape.new()
				if err := json.Unmarshal([]byte(fmt.Sprintf(shape.wire, field)), value); err != nil {
					t.Fatal(err)
				}
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var object map[string]json.RawMessage
				if err := json.Unmarshal(encoded, &object); err != nil {
					t.Fatal(err)
				}
				if shape.name == "permission" {
					nested := object["toolCall"]
					object = nil
					if err := json.Unmarshal(nested, &object); err != nil {
						t.Fatal(err)
					}
				}
				want := ""
				if field != "" && field != `,"name":null` {
					want = field[len(`,"name":`):]
				}
				if got := string(object["name"]); got != want {
					t.Fatalf("name = %s, want %s in %s", got, want, encoded)
				}
			})
		}
	}
}

func TestToolCallNameHelpers(t *testing.T) {
	t.Parallel()
	for _, update := range []SessionUpdate{
		StartToolCall("call-1", "Read", WithStartName("read_file")),
		UpdateToolCall("call-1", WithUpdateName("read_file")),
	} {
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		var decoded SessionUpdate
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		var name *string
		if decoded.ToolCall != nil {
			name = decoded.ToolCall.Name
		} else if decoded.ToolCallUpdate != nil {
			name = decoded.ToolCallUpdate.Name
		}
		if name == nil || *name != "read_file" {
			t.Fatalf("tool name was lost: %s", encoded)
		}
	}
}
