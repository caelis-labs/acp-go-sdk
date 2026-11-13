package interop_test

import (
	"encoding/json"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/caelis-labs/acp-go-sdk/internal/interoptest/contract"
)

var contractCaseNames = []string{"v1-full", "v1-append", "v1-null", "v1-omitted", "v2-full", "v2-append", "v2-null", "v2-omitted", "v2-no-session", "v2-null-session", "v2-lifecycle", "v2-after", "v2-newer", "v2-content"}

func TestContractCasesCoverPinnedSurface(t *testing.T) {
	dir := filepath.Join(repositoryRoot, "interop/contract")
	seen := map[string]bool{}
	for _, name := range contractCaseNames {
		var c contract.Case
		if err := contract.Read(filepath.Join(dir, name+".json"), &c); err != nil {
			t.Fatal(err)
		}
		if c.Name != name || len(c.Steps) == 0 {
			t.Fatalf("invalid case %s", name)
		}
		errors := 0
		for _, s := range c.Steps {
			if c.Version == 2 {
				seen[s.Method] = true
			}
			if s.Error != 0 && len(s.Updates) > 0 && !s.Cancel {
				t.Fatalf("%s: rejected requests must not expect updates", name)
			}
			if s.Error != 0 {
				errors++
			}
		}
		if name == "v1-full" || name == "v2-full" {
			if errors < 10 {
				t.Fatalf("%s missing rejection coverage", name)
			}
		}
	}
	var schema struct {
		Defs map[string]struct {
			Side   string `json:"x-side"`
			Method string `json:"x-method"`
			AnyOf  []struct {
				Properties map[string]struct {
					Const string `json:"const"`
				} `json:"properties"`
			} `json:"anyOf"`
		} `json:"$defs"`
	}
	if err := contract.Read(filepath.Join(repositoryRoot, "schema/v2/schema.json"), &schema); err != nil {
		t.Fatal(err)
	}
	for _, d := range schema.Defs {
		if d.Side == "agent" && d.Method != "" && !seen[d.Method] {
			t.Errorf("missing v2 method %s", d.Method)
		}
	}
	var vectors []map[string]any
	if err := contract.Read(filepath.Join(dir, "updates.json"), &vectors); err != nil {
		t.Fatal(err)
	}
	updates := map[string]bool{}
	for _, v := range vectors {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var typed v2.SessionUpdate
		if err := json.Unmarshal(raw, &typed); err != nil {
			t.Fatalf("invalid update vector: %v", err)
		}
		roundTrip, err := json.Marshal(typed)
		if err != nil {
			t.Fatal(err)
		}
		var got any
		if err := json.Unmarshal(roundTrip, &got); err != nil {
			t.Fatal(err)
		}
		if err := contract.Match(v, got); err != nil {
			t.Fatalf("v2 update codec lost fields: %v", err)
		}
		name, _ := v["sessionUpdate"].(string)
		updates[name] = true
	}
	for _, branch := range schema.Defs["SessionUpdate"].AnyOf {
		if name := branch.Properties["sessionUpdate"].Const; name != "" && !updates[name] {
			t.Errorf("missing v2 update variant %s", name)
		}
	}
	var reverse []struct {
		Method string `json:"method"`
	}
	if err := contract.Read(filepath.Join(dir, "reverse.json"), &reverse); err != nil {
		t.Fatal(err)
	}
	clientMethods := map[string]bool{"session/update": true}
	for _, r := range reverse {
		clientMethods[r.Method] = true
	}
	for _, d := range schema.Defs {
		if d.Side == "client" && d.Method != "" && !clientMethods[d.Method] {
			t.Errorf("missing v2 client method %s", d.Method)
		}
	}
	// Ensure fixtures remain ordinary JSON consumable by all three clients.
	for _, name := range []string{"reverse.json", "updates.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !json.Valid(b) {
			t.Fatalf("invalid shared wire vector %s", name)
		}
	}
}
