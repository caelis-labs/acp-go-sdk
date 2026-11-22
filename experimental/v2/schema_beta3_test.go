package v2

import (
	"encoding/json"
	"testing"
)

func TestBeta3PayloadConstraints(t *testing.T) {
	for _, test := range []struct {
		name    string
		target  any
		valid   string
		invalid []string
	}{
		{
			"request scope", new(ElicitationRequestScope),
			`{"requestId":"request","future":true}`,
			[]string{`{"requestId":"request","sessionId":null}`, `{"requestId":"request","toolCallId":"tool"}`},
		},
		{
			"session scope", new(ElicitationSessionScope),
			`{"sessionId":"session","toolCallId":"tool"}`,
			[]string{`{"sessionId":"session","requestId":null}`},
		},
		{
			"titled items", new(TitledMultiSelectItems),
			`{"anyOf":[{"const":"value","title":"Value"}]}`,
			[]string{`{"anyOf":[{"const":"value","title":"Value"}],"type":"string"}`},
		},
		{
			"negotiation error", new(Error),
			`{"code":-32022,"message":"Unsupported version","data":{"requested":1,"supported":[2]}}`,
			[]string{
				`{"code":-32022,"message":"Unsupported version"}`,
				`{"code":-32022,"message":"Unsupported version","data":null}`,
				`{"code":-32022,"message":"Unsupported version","data":{"requested":1}}`,
			},
		},
		{
			"system prompt", new(SystemPrompt),
			`{"mode":"append","content":[{"type":"text","text":"instructions"}]}`,
			[]string{`{"mode":"append","content":[]}`},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.valid), test.target); err != nil {
				t.Fatalf("valid payload rejected: %v", err)
			}
			for _, wire := range test.invalid {
				if err := json.Unmarshal([]byte(wire), test.target); err == nil {
					t.Errorf("invalid payload accepted: %s", wire)
				}
			}
		})
	}
}

func TestBeta3MarkdownPlanRequiresContent(t *testing.T) {
	var plan PlanUpdateContent
	if err := json.Unmarshal([]byte(`{"type":"markdown","planId":"p"}`), &plan); err == nil {
		t.Fatal("accepted a Markdown plan without content")
	}
	if err := json.Unmarshal([]byte(`{"type":"markdown","planId":"p","content":"- Review changes"}`), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Markdown == nil || plan.Markdown.PlanId != "p" || plan.Markdown.Content != "- Review changes" {
		t.Fatalf("unexpected Markdown plan: %+v", plan)
	}
}
