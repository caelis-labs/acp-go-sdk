package v2

import (
	"encoding/json"
	"testing"
)

type opaqueUpdate interface {
	json.Marshaler
	json.Unmarshaler
	RawInputState() NullableFieldState
	RawOutputState() NullableFieldState
	SetRawInput(any)
	SetRawOutput(any)
	ClearRawInput()
	ClearRawOutput()
	UnsetRawInput()
	UnsetRawOutput()
}

func TestToolCallOpaqueJSONPresence(t *testing.T) {
	for _, newUpdate := range []func() opaqueUpdate{func() opaqueUpdate { return new(ToolCallUpdate) }, func() opaqueUpdate { return new(SessionToolCallUpdate) }} {
		for _, tc := range []struct {
			wire  string
			state NullableFieldState
		}{
			{`{"sessionUpdate":"tool_call_update","toolCallId":"t1"}`, NullableFieldAbsent},
			{`{"sessionUpdate":"tool_call_update","toolCallId":"t1","rawInput":null,"rawOutput":null}`, NullableFieldNull},
			{`{"sessionUpdate":"tool_call_update","toolCallId":"t1","rawInput":{"keep":[true,42]},"rawOutput":false}`, NullableFieldValue},
		} {
			u := newUpdate()
			if err := json.Unmarshal([]byte(tc.wire), u); err != nil {
				t.Fatal(err)
			}
			if u.RawInputState() != tc.state || u.RawOutputState() != tc.state {
				t.Fatalf("presence lost for %s", tc.wire)
			}
			encoded, err := json.Marshal(u)
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.wire), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"rawInput", "rawOutput"} {
				if string(got[key]) != string(want[key]) {
					t.Fatalf("%s lost: %s", key, encoded)
				}
			}
		}
		u := newUpdate()
		u.SetRawInput(map[string]any{"keep": true})
		u.SetRawOutput(false)
		if u.RawInputState() != NullableFieldValue || u.RawOutputState() != NullableFieldValue {
			t.Fatal("set did not retain values")
		}
		u.ClearRawInput()
		u.ClearRawOutput()
		if u.RawInputState() != NullableFieldNull || u.RawOutputState() != NullableFieldNull {
			t.Fatal("clear not explicit null")
		}
		u.UnsetRawInput()
		u.UnsetRawOutput()
		if u.RawInputState() != NullableFieldAbsent || u.RawOutputState() != NullableFieldAbsent {
			t.Fatal("unset not absent")
		}
		u.SetRawOutput(make(chan int))
		if _, err := json.Marshal(u); err == nil {
			t.Fatal("marshal error was hidden")
		}
	}
}
