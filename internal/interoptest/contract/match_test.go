package contract

import (
	"encoding/json"
	"testing"
)

func TestMatchCannotAcceptIncompleteEvidence(t *testing.T) {
	for _, tc := range []struct {
		want, got string
		ok        bool
	}{
		{`{"mode":"append"}`, `{"mode":"append","extra":42}`, true},
		{`{"x":null}`, `{}`, false}, {`{"x":null}`, `{"x":false}`, false},
		{`{"x":[]}`, `{"x":[1]}`, false}, {`[1]`, `[1,2]`, false}, {`[1,2]`, `[1]`, false},
		{`{}`, `null`, false}, {`{"a":{"b":true}}`, `{"a":{"b":false}}`, false},
	} {
		var w, g any
		if err := json.Unmarshal([]byte(tc.want), &w); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tc.got), &g); err != nil {
			t.Fatal(err)
		}
		if ok := Match(w, g) == nil; ok != tc.ok {
			t.Errorf("%s vs %s: got %v", tc.want, tc.got, ok)
		}
	}
}
