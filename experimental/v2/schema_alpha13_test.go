package v2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestAlpha13ArbitraryJSONFallbacks(t *testing.T) {
	for _, input := range []string{`null`, `{"future":[9007199254740993,false,null]}`, `[1,{"nested":true}]`} {
		for _, value := range []any{&ElicitationContentValue{}, &ErrorData{}} {
			if err := json.Unmarshal([]byte(input), value); err != nil {
				t.Fatalf("%T: %s: %v", value, input, err)
			}
			encoded, err := json.Marshal(value)
			if err != nil || !jsonEqualForTest(encoded, []byte(input)) {
				t.Fatalf("%T: %s: %v", value, encoded, err)
			}
		}
	}
	for _, input := range []string{`{"requested":3,"supported":[2]}`, `{"sessionId":"missing"}`} {
		var data ErrorData
		if err := json.Unmarshal([]byte(input), &data); err != nil || data.Other != nil {
			t.Fatalf("defined error data: %+v %v", data, err)
		}
	}
}

func TestAlpha13ErrorEnvelopeNullAndPreciseIDs(t *testing.T) {
	for _, id := range []string{`null`, `9007199254740993`, `"request"`} {
		input := `{"id":` + id + `,"error":{"code":-32600,"message":"Invalid request"}}`
		for _, value := range []any{&AgentResponse{}, &ClientResponse{}, &AgentError{}, &ClientError{}} {
			if err := json.Unmarshal([]byte(input), value); err != nil {
				t.Fatalf("%T: %v", value, err)
			}
			encoded, err := json.Marshal(value)
			if err != nil || !jsonEqualForTest(encoded, []byte(input)) {
				t.Fatalf("%T: %s %v", value, encoded, err)
			}
		}
	}
	for _, id := range []string{`true`, `1.5`, `9223372036854775808`} {
		var value AgentResponse
		if json.Unmarshal([]byte(`{"id":`+id+`,"error":{"code":-32600,"message":"invalid"}}`), &value) == nil {
			t.Fatalf("accepted invalid error ID %s", id)
		}
	}
}

func TestAlpha13ElicitationRequiresSchemaFields(t *testing.T) {
	for _, input := range []string{`{}`, `{"type":"object"}`, `{"properties":{}}`, `{"type":"object","properties":null}`} {
		var schema ElicitationSchema
		if json.Unmarshal([]byte(input), &schema) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	var schema ElicitationSchema
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{}}`), &schema); err != nil {
		t.Fatal(err)
	}
}

func TestAlpha13SelectMinimumAfterRecovery(t *testing.T) {
	for _, input := range []string{`[]`, `null`, `[{"groupId":"g","name":"Group","options":[]}]`, `[{"groupId":"g","name":"Group","options":[null,{}]}]`} {
		var options SessionConfigSelectOptions
		if json.Unmarshal([]byte(input), &options) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	var options SessionConfigSelectOptions
	if err := json.Unmarshal([]byte(`[{"groupId":"g","name":"Group","options":[null,{}, {"name":"A","value":"a"}]}]`), &options); err != nil || options.Grouped == nil || len((*options.Grouped)[0].Options) != 1 {
		t.Fatalf("valid option recovery: %+v %v", options, err)
	}
	var update ConfigOptionUpdate
	if err := json.Unmarshal([]byte(`{"configOptions":[{"type":"select","configId":"bad","name":"Bad","currentValue":"a","options":[]},{"type":"select","configId":"ok","name":"Good","currentValue":"a","options":[{"name":"A","value":"a"}]}]}`), &update); err != nil || len(update.ConfigOptions) != 1 {
		t.Fatalf("invalid select recovery: %+v %v", update, err)
	}
}

func TestAlpha13PromptMustHaveContent(t *testing.T) {
	for _, input := range []string{`[]`, `null`} {
		var request PromptRequest
		if json.Unmarshal([]byte(`{"sessionId":"s","prompt":`+input+`}`), &request) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	request := PromptRequest{SessionId: "s", Prompt: []ContentBlock{}}
	if request.Validate() == nil {
		t.Fatal("accepted outgoing empty prompt")
	}
}

func TestAlpha13BackgroundAndCostPatches(t *testing.T) {
	background := `[{"type":"tool_call","toolCallId":"t"},{"type":"terminal","terminalId":"term"},{"type":"future","n":9007199254740993}]`
	for _, value := range []any{&RunningStateUpdate{}, &RequiresActionStateUpdate{}, &IdleStateUpdate{}} {
		input := `{"sessionId":"s","stopReason":"end_turn","background":` + background + `}`
		if err := json.Unmarshal([]byte(input), value); err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		encoded, err := json.Marshal(value)
		var fields map[string]json.RawMessage
		if err != nil || json.Unmarshal(encoded, &fields) != nil || !jsonEqualForTest(fields["background"], []byte(background)) {
			t.Fatalf("%T: %s %v", value, encoded, err)
		}
	}
	for _, tc := range []struct {
		field string
		state NullableFieldState
	}{
		{"", NullableFieldAbsent}, {`,"cost":null`, NullableFieldNull},
		{`,"cost":{"amount":1.5,"currency":"credits"}`, NullableFieldValue},
		{`,"cost":false`, NullableFieldAbsent},
	} {
		var usage UsageUpdate
		if err := json.Unmarshal([]byte(`{"used":1,"size":100`+tc.field+`}`), &usage); err != nil || usage.CostState() != tc.state {
			t.Fatalf("cost %s: %+v %v", tc.field, usage, err)
		}
		encoded, err := json.Marshal(usage)
		if err != nil {
			t.Fatal(err)
		}
		var decoded UsageUpdate
		if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.CostState() != tc.state {
			t.Fatalf("cost roundtrip: %s %v", encoded, err)
		}
	}
}

type entityCancelAgent struct{ alpha9Agent }

func (*entityCancelAgent) CancelToolCall(_ context.Context, req CancelToolCallRequest) (CancelToolCallResponse, error) {
	if req.SessionId != "s" || req.ToolCallId != "t" {
		return CancelToolCallResponse{}, acp.NewInvalidParams(nil)
	}
	return CancelToolCallResponse{}, nil
}

func (*entityCancelAgent) CancelTerminal(_ context.Context, req CancelTerminalRequest) (CancelTerminalResponse, error) {
	if req.SessionId != "s" || req.TerminalId != "term" {
		return CancelTerminalResponse{}, acp.NewInvalidParams(nil)
	}
	return CancelTerminalResponse{}, nil
}

func TestAlpha13EntityCancellationConnections(t *testing.T) {
	for _, implementation := range []Agent{&entityCancelAgent{}, &alpha9Agent{}} {
		t.Run(fmt.Sprintf("%T", implementation), func(t *testing.T) {
			a, b := net.Pipe()
			agent, err := NewAgentSideConnection(implementation, a, a)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = agent.Close() }()
			client, err := NewClientSideConnection(&loopbackClient{}, b, b)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, toolErr := client.CancelToolCall(ctx, CancelToolCallRequest{SessionId: "s", ToolCallId: "t"})
			_, terminalErr := client.CancelTerminal(ctx, CancelTerminalRequest{SessionId: "s", TerminalId: "term"})
			for _, err := range []error{toolErr, terminalErr} {
				if _, supported := implementation.(*entityCancelAgent); supported {
					if err != nil {
						t.Fatal(err)
					}
				} else {
					var requestErr *acp.RequestError
					if !errors.As(err, &requestErr) || requestErr.Code != -32602 {
						t.Fatalf("default cancellation: %v", err)
					}
				}
			}
		})
	}
}

func TestAlpha13TypedConnectionsRejectWholeBatch(t *testing.T) {
	for _, role := range []string{"agent", "client"} {
		t.Run(role, func(t *testing.T) {
			a, b := net.Pipe()
			var closeConnection func() error
			if role == "agent" {
				conn, err := NewAgentSideConnection(&alpha9Agent{}, a, a)
				if err != nil {
					t.Fatal(err)
				}
				closeConnection = conn.Close
			} else {
				conn, err := NewClientSideConnection(&loopbackClient{}, a, a)
				if err != nil {
					t.Fatal(err)
				}
				closeConnection = conn.Close
			}
			defer func() { _ = closeConnection() }()
			defer func() { _ = b.Close() }()
			if err := b.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(b)
			for _, input := range []string{`[{"jsonrpc":"2.0","id":1,"method":"session/new","params":{"cwd":"/"}}]`, `[]`, `[{"jsonrpc":"2.0","method":"session/cancel","params":{"sessionId":"s"}}]`} {
				if _, err := fmt.Fprintln(b, input); err != nil {
					t.Fatal(err)
				}
				line, err := reader.ReadBytes('\n')
				var response struct {
					ID    json.RawMessage   `json:"id"`
					Error *acp.RequestError `json:"error"`
				}
				if err != nil || json.Unmarshal(line, &response) != nil || response.Error == nil || response.Error.Code != -32600 || string(response.ID) != "null" {
					t.Fatalf("batch response: %s %v", line, err)
				}
			}
		})
	}
}
