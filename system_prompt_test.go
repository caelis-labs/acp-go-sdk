package acp_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	acp "github.com/caelis-labs/acp-go-sdk"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
)

// Exercise the two independently generated profiles with the same wire fixtures.
func TestSystemPromptProfiles(t *testing.T) {
	for _, profile := range []struct {
		name         string
		prompt, caps func() any
		requests     []func() any
	}{
		{"stable", func() any { return new(acp.SystemPrompt) }, func() any { return new(acp.SessionCapabilities) }, []func() any{func() any { return new(acp.NewSessionRequest) }, func() any { return new(acp.LoadSessionRequest) }, func() any { return new(acp.ResumeSessionRequest) }}},
		{"v2", func() any { return new(v2.SystemPrompt) }, func() any { return new(v2.SessionCapabilities) }, []func() any{func() any { return new(v2.NewSessionRequest) }, func() any { return new(v2.ResumeSessionRequest) }}},
	} {
		t.Run(profile.name, func(t *testing.T) {
			for _, wire := range []string{`{}`, `{"systemPrompt":null}`, `{"systemPrompt":{}}`, `{"systemPrompt":{"mode":{"override":{}}}}`} {
				caps := profile.caps()
				if err := json.Unmarshal([]byte(wire), caps); err != nil {
					t.Fatal(err)
				}
				field := reflect.ValueOf(caps).Elem().FieldByName("SystemPrompt")
				if field.IsNil() != (wire == `{}` || wire == `{"systemPrompt":null}`) {
					t.Fatalf("capability presence lost: %s", wire)
				}
				data, err := json.Marshal(caps)
				if err != nil {
					t.Fatal(err)
				}
				if !field.IsNil() && !bytes.Contains(data, []byte(`"systemPrompt":{`)) {
					t.Fatalf("empty capability not advertised: %s", data)
				}
			}
			for _, newRequest := range profile.requests {
				for _, optional := range []string{"", `,"systemPrompt":null`} {
					request := newRequest()
					wire := []byte(`{"cwd":"/tmp","mcpServers":[],"sessionId":"test"` + optional + `}`)
					if err := json.Unmarshal(wire, request); err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(request)
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Contains(data, []byte(`"systemPrompt"`)) {
						t.Fatalf("absent prompt emitted: %s", data)
					}
				}
			}
			for _, wire := range []string{`{}`, `{"mode":"append"}`, `{"content":[]}`, `{"mode":null,"content":[]}`, `{"mode":"append","content":null}`} {
				if err := json.Unmarshal([]byte(wire), profile.prompt()); err == nil {
					t.Fatalf("accepted invalid prompt: %s", wire)
				}
			}
			for _, mode := range []string{"append", "override", "future-mode", "_custom"} {
				wire := []byte(`{"mode":"` + mode + `","content":[{"type":"text","text":"instructions"}],"_meta":{"vendor":{"value":42}}}`)
				prompt := profile.prompt()
				if err := json.Unmarshal(wire, prompt); err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(prompt)
				if err != nil {
					t.Fatal(err)
				}
				var want, got any
				if err := json.Unmarshal(wire, &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(data, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("prompt changed: %s", data)
				}
				for _, newRequest := range profile.requests {
					requestWire := []byte(`{"cwd":"/tmp","mcpServers":[],"sessionId":"test","systemPrompt":` + string(wire) + `}`)
					request := newRequest()
					if err := json.Unmarshal(requestWire, request); err != nil {
						t.Fatal(err)
					}
					data, err := json.Marshal(request)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(data, &fields); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(fields["systemPrompt"], &got); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("lifecycle prompt lost: %s", data)
					}
				}
			}
		})
	}
}
