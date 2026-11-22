package acp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStablePlanAndSubagentCapabilities(t *testing.T) {
	for _, test := range []struct {
		wire      string
		supported bool
	}{
		{`{}`, false},
		{`{"plan":null,"subagents":null}`, false},
		{`{"plan":false,"subagents":[]}`, false},
		{`{"plan":{},"subagents":{}}`, true},
	} {
		var caps ClientCapabilities
		if err := json.Unmarshal([]byte(test.wire), &caps); err != nil {
			t.Fatal(err)
		}
		if (caps.Plan != nil) != test.supported || (caps.Subagents != nil) != test.supported {
			t.Fatalf("unexpected capability support for %s: %+v", test.wire, caps)
		}
	}
}

func TestStableSessionPatchRoundTrips(t *testing.T) {
	for _, wire := range []string{
		`{"sessionUpdate":"subagent_update","sessionId":"child","additionalDirectories":[],"_meta":null,"state":{"state":"_future","id":9007199254740993}}`,
		`{"sessionUpdate":"subagent_update","sessionId":"child","capabilities":null,"_meta":{"id":9007199254740993}}`,
		`{"sessionUpdate":"session_message","messageId":"message","senderSessionId":null,"content":null,"_meta":null}`,
		`{"sessionUpdate":"session_message","messageId":"message","content":[],"_meta":{"id":9007199254740993}}`,
	} {
		var update SessionUpdate
		if err := json.Unmarshal([]byte(wire), &update); err != nil {
			t.Fatal(err)
		}
		if update.SubagentUpdate == nil && update.SessionMessage == nil {
			t.Fatalf("expected a typed session update for %s", wire)
		}
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		var want, got map[string]json.RawMessage
		if err := json.Unmarshal([]byte(wire), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip = %s, want %s", encoded, wire)
		}
	}
}
