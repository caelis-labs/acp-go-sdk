package acp

import (
	"encoding/json"
	"math"
	"testing"
)

var (
	_ *ToolCallId = (&CreateElicitationForm{}).ToolCallId
	_ *ToolCallId = (&CreateElicitationUrl{}).ToolCallId
)

func TestElicitationOtherPreservesRawPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		target  any
	}{
		{
			name:    "request",
			payload: `{"mode":"_vendor","message":"m","sessionId":"s","vendor":{"x":9007199254740993}}`,
			target:  &CreateElicitationRequest{},
		},
		{
			name:    "response",
			payload: `{"action":"_vendor","vendor":{"x":9007199254740993}}`,
			target:  &CreateElicitationResponse{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.payload), test.target); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(test.target)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(encoded); got != test.payload {
				t.Fatalf("round trip = %s, want %s", got, test.payload)
			}
		})
	}
}

func TestElicitationOtherRequiresScope(t *testing.T) {
	for _, invalid := range []string{
		`{"mode":"_vendor","message":"m","vendor":true}`,
		`{"mode":"_vendor","message":"m","sessionId":null,"vendor":true}`,
	} {
		var request CreateElicitationRequest
		if err := json.Unmarshal([]byte(invalid), &request); err == nil {
			t.Fatalf("invalid custom elicitation request was accepted: %s", invalid)
		}
	}
}

func TestKnownElicitationPreservesTypedScope(t *testing.T) {
	payload := []byte(`{"mode":"form","message":"m","sessionId":"s","requestedSchema":{"type":"object","properties":{}}}`)
	var request CreateElicitationRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatal(err)
	}
	if request.Form == nil || request.Form.SessionId == nil || *request.Form.SessionId != SessionId("s") {
		t.Fatalf("typed session scope = %#v", request.Form)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if got := string(fields["sessionId"]); got != `"s"` {
		t.Fatalf("round-trip sessionId = %s", got)
	}

	for _, invalid := range []string{
		`{"mode":"form","message":"m","requestedSchema":{"properties":{}}}`,
		`{"mode":"form","message":"m","sessionId":null,"requestedSchema":{"properties":{}}}`,
		`{"mode":"url","message":"m","elicitationId":"e","url":"https://example.com"}`,
	} {
		var unscoped CreateElicitationRequest
		if err := json.Unmarshal([]byte(invalid), &unscoped); err == nil {
			t.Fatalf("unscoped known elicitation accepted: %s", invalid)
		}
	}
}

func TestErrorCodeConstUnionDispatch(t *testing.T) {
	var known ErrorCode
	if err := json.Unmarshal([]byte(`-32601`), &known); err != nil {
		t.Fatal(err)
	}
	if known.MethodNotFound == nil || known.ParseError != nil || known.Other != nil {
		t.Fatalf("known error code decoded to %#v", known)
	}

	var other ErrorCode
	if err := json.Unmarshal([]byte(`-31999`), &other); err != nil {
		t.Fatal(err)
	}
	if other.Other == nil || other.ParseError != nil {
		t.Fatalf("unknown error code decoded to %#v", other)
	}
}

func TestSchemaIntegerFormatsPreserveFullRanges(t *testing.T) {
	var requestID RequestId
	if err := json.Unmarshal([]byte(`9223372036854775807`), &requestID); err != nil {
		t.Fatal(err)
	}
	if requestID.Number == nil || *requestID.Number != RequestIdNumber(math.MaxInt64) {
		t.Fatalf("request id = %#v", requestID)
	}
	encoded, err := json.Marshal(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `9223372036854775807` {
		t.Fatalf("request id round trip = %s", got)
	}

	var usage UsageUpdate
	if err := json.Unmarshal([]byte(`{"size":18446744073709551615,"used":18446744073709551615}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Size != math.MaxUint64 || usage.Used != math.MaxUint64 {
		t.Fatalf("usage = %#v", usage)
	}

	for name, payload := range map[string]string{
		"terminal byte limit": `{"command":"cmd","outputByteLimit":18446744073709551615,"sessionId":"s"}`,
		"resource size":       `{"name":"n","size":9223372036854775807,"uri":"file:///n"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var target any
			if name == "terminal byte limit" {
				target = &CreateTerminalRequest{}
			} else {
				target = &ResourceLink{}
			}
			if err := json.Unmarshal([]byte(payload), target); err != nil {
				t.Fatal(err)
			}
		})
	}

	var version ProtocolVersion
	if err := json.Unmarshal([]byte(`65535`), &version); err != nil || version != math.MaxUint16 {
		t.Fatalf("max protocol version = %d, err = %v", version, err)
	}
	if err := json.Unmarshal([]byte(`65536`), &version); err == nil {
		t.Fatal("protocol version overflow was accepted")
	}
}

func TestMetaPreservesJSONNumbersAcrossTypedAndUnionValues(t *testing.T) {
	t.Run("typed object", func(t *testing.T) {
		payload := []byte(`{"content":"ok","_meta":{"large":9007199254740993,"nested":{"large":18446744073709551615}}}`)
		var response ReadTextFileResponse
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatal(err)
		}
		if got := string(response.Meta["large"]); got != `9007199254740993` {
			t.Fatalf("meta large = %s", got)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if got := string(fields["_meta"]); got != `{"large":9007199254740993,"nested":{"large":18446744073709551615}}` {
			t.Fatalf("round-trip meta = %s", got)
		}
	})

	t.Run("content union", func(t *testing.T) {
		payload := []byte(`{"type":"text","text":"hello","annotations":{"audience":["user"]},"_meta":{"large":9007199254740993}}`)
		var block ContentBlock
		if err := json.Unmarshal(payload, &block); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(block)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if got := string(fields["_meta"]); got != `{"large":9007199254740993}` {
			t.Fatalf("round-trip meta = %s", got)
		}
		if _, ok := fields["annotations"]; !ok {
			t.Fatalf("annotations dropped from content union: %s", encoded)
		}
	})
}

func TestOpenSchemaVariantsPreserveRawPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		target  any
	}{
		{
			name:    "elicitation property",
			payload: `{"type":"_vendor","future":{"large":9007199254740993}}`,
			target:  &ElicitationPropertySchema{},
		},
		{
			name:    "multi-select items",
			payload: `{"type":"_vendor","future":{"large":9007199254740993}}`,
			target:  &MultiSelectItems{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.payload), test.target); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(test.target)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(encoded); got != test.payload {
				t.Fatalf("round trip = %s, want %s", got, test.payload)
			}
		})
	}
}

func TestOpenSchemaVariantsRejectInvalidDiscriminatorTypes(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		target  any
	}{
		{name: "elicitation property", payload: `{"type":123}`, target: &ElicitationPropertySchema{}},
		{name: "multi-select items", payload: `{"type":123}`, target: &MultiSelectItems{}},
		{name: "elicitation request", payload: `{"mode":123,"message":"m","sessionId":"s"}`, target: &CreateElicitationRequest{}},
		{name: "elicitation response", payload: `{"action":123}`, target: &CreateElicitationResponse{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(test.payload), test.target); err == nil {
				t.Fatalf("schema-invalid discriminator accepted: %s", test.payload)
			}
		})
	}
}

func TestUnionUnmarshalResetsReusedReceiver(t *testing.T) {
	var response AgentResponse
	if err := json.Unmarshal([]byte(`{"id":1,"result":{"ok":true}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result == nil || response.Error != nil {
		t.Fatalf("result response = %#v", response)
	}
	if err := json.Unmarshal([]byte(`{"id":1,"error":{"code":-32601,"message":"missing"}}`), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result != nil || response.Error == nil {
		t.Fatalf("error response retained stale variant: %#v", response)
	}
	if err := json.Unmarshal([]byte(`{}`), &response); err == nil {
		t.Fatal("invalid response was accepted")
	}
	if response.Result != nil || response.Error != nil {
		t.Fatalf("failed decode retained stale variant: %#v", response)
	}
}

func TestRequiredPropertiesUseWirePresence(t *testing.T) {
	for _, payload := range []string{`{}`, `{"protocolVersion":null}`} {
		var request InitializeRequest
		if err := json.Unmarshal([]byte(payload), &request); err == nil {
			t.Fatalf("invalid initialize request accepted: %s", payload)
		}
	}
	var request InitializeRequest
	if err := json.Unmarshal([]byte(`{"protocolVersion":0}`), &request); err != nil {
		t.Fatalf("present zero protocol version rejected: %v", err)
	}

	for _, payload := range []string{`{"sessionId":"s"}`, `{"sessionId":"s","update":null}`} {
		var notification SessionNotification
		if err := json.Unmarshal([]byte(payload), &notification); err == nil {
			t.Fatalf("invalid session notification accepted: %s", payload)
		}
	}

	var terminal CreateTerminalRequest
	if err := json.Unmarshal([]byte(`{"command":"","sessionId":""}`), &terminal); err != nil {
		t.Fatalf("present empty strings rejected without a schema minLength: %v", err)
	}
}

func TestValidateChecksConstructedNonNullContainersAndUnions(t *testing.T) {
	if err := (&PromptRequest{}).Validate(); err == nil {
		t.Fatal("nil required prompt was accepted")
	}
	if err := (&PromptRequest{Prompt: []ContentBlock{}}).Validate(); err != nil {
		t.Fatalf("present empty prompt rejected: %v", err)
	}
	if err := (&PromptRequest{Prompt: []ContentBlock{{}}}).Validate(); err == nil {
		t.Fatal("empty content union was accepted")
	}
	if err := (&SessionNotification{}).Validate(); err == nil {
		t.Fatal("empty required session update union was accepted")
	}
	if err := (&SessionNotification{Update: SessionUpdate{Plan: &SessionUpdatePlan{}}}).Validate(); err == nil {
		t.Fatal("selected session update with nil required entries was accepted")
	}
	if err := (&ToolCallUpdate{}).Validate(); err != nil {
		t.Fatalf("present empty toolCallId rejected without a schema minLength: %v", err)
	}
}
