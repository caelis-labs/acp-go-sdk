package v2

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAlpha17PromptIdempotencyKeyDecode(t *testing.T) {
	const prompt = `{"sessionId":"s","prompt":[{"type":"text","text":"hello"}]`
	// JSON Schema length counts Unicode characters.
	maximum := strings.Repeat("界", 255)
	for _, test := range []struct {
		name  string
		field string
		want  string
		valid bool
	}{
		{name: "omitted", valid: true},
		{name: "null", field: `,"idempotencyKey":null`, valid: true},
		{name: "minimum", field: `,"idempotencyKey":"A"`, want: "A", valid: true},
		{name: "maximum", field: `,"idempotencyKey":"` + maximum + `"`, want: maximum, valid: true},
		{name: "empty", field: `,"idempotencyKey":""`},
		{name: "too long", field: `,"idempotencyKey":"` + maximum + `界"`},
		{name: "wrong type", field: `,"idempotencyKey":42`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request PromptRequest
			err := json.Unmarshal([]byte(prompt+test.field+"}"), &request)
			if !test.valid {
				if err == nil {
					t.Fatal("accepted an invalid idempotency key")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if request.IdempotencyKey != nil {
					t.Fatalf("expected an absent key, got %q", *request.IdempotencyKey)
				}
			} else if request.IdempotencyKey == nil || string(*request.IdempotencyKey) != test.want {
				t.Fatalf("key did not survive decoding: %v", request.IdempotencyKey)
			}
		})
	}
}

func TestAlpha17DiffRequiresAValidChange(t *testing.T) {
	for _, changes := range []string{`[]`, `[null,{"operation":"modify"}]`} {
		var diff Diff
		if json.Unmarshal([]byte(`{"changes":`+changes+"}"), &diff) == nil {
			t.Fatalf("accepted diff changes %s", changes)
		}
	}
	var diff Diff
	input := `{"changes":[null,{"operation":"modify","path":"/file"}]}`
	if err := json.Unmarshal([]byte(input), &diff); err != nil || len(diff.Changes) != 1 || diff.Changes[0].Modify == nil {
		t.Fatalf("valid change was lost during recovery: %+v, %v", diff, err)
	}
}
