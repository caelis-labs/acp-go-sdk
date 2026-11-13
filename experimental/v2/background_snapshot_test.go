package v2

import (
	"encoding/json"
	"testing"
)

func TestBackgroundSnapshotEmptyIsDistinctFromUnreported(t *testing.T) {
	for _, factory := range []func() any{func() any { return new(RunningStateUpdate) }, func() any { return new(IdleStateUpdate) }, func() any { return new(RequiresActionStateUpdate) }, func() any { return new(SessionUpdate) }} {
		for _, tc := range []struct {
			extra   string
			present bool
		}{
			{``, false}, {`,"background":null`, false}, {`,"background":{}`, false}, {`,"background":[]`, true},
		} {
			v := factory()
			raw := []byte(`{"sessionUpdate":"state_update","state":"idle"` + tc.extra + `}`)
			if err := json.Unmarshal(raw, v); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			actual, ok := fields["background"]
			if ok != tc.present {
				t.Fatalf("snapshot presence changed: %s -> %s", raw, encoded)
			}
			if tc.present && string(actual) != "[]" {
				t.Fatalf("empty snapshot changed: %s", encoded)
			}
		}
	}
}
