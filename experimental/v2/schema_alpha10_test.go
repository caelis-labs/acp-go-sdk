package v2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestAlpha10CommandDiscriminatorAndUnknownPayload(t *testing.T) {
	for _, input := range []string{
		`{"type":"command","name":"help","description":"Help"}`,
		`{"type":"future","name":"future","description":"Future","input":42,"_meta":false,"n":9007199254740993}`,
	} {
		var command AvailableCommand
		if err := json.Unmarshal([]byte(input), &command); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(command)
		if err != nil || !jsonEqualForTest(encoded, []byte(input)) {
			t.Fatalf("roundtrip: %s %v", encoded, err)
		}
	}
	for _, input := range []string{`{"name":"help","description":"Help"}`, `{"type":"command","name":"help"}`, `{"type":null,"name":"help","description":"Help"}`} {
		var command AvailableCommand
		if json.Unmarshal([]byte(input), &command) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestAlpha10ReplacementLists(t *testing.T) {
	for _, tc := range []struct {
		name, key, prefix, valid string
		newValue                 func() any
	}{
		{"commands", "availableCommands", ``, `{"type":"command","name":"help","description":"Help"}`, func() any { return &AvailableCommandsUpdate{} }},
		{"config", "configOptions", ``, `{"type":"select","configId":"mode","name":"Mode","currentValue":"ask","options":[{"value":"ask","name":"Ask"}]}`, func() any { return &ConfigOptionUpdate{} }},
		{"plan", "entries", `"planId":"p",`, `{"content":"Step","priority":"medium","status":"pending"}`, func() any { return &PlanItems{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, value := range []string{`null`, `{}`, `"bad"`, `false`, `1`} {
				input := `{` + tc.prefix + `"` + tc.key + `":` + value + `}`
				if json.Unmarshal([]byte(input), tc.newValue()) == nil {
					t.Fatalf("accepted replacement %s", input)
				}
			}
			input := `{` + tc.prefix + `"` + tc.key + `":[null,{},42,` + tc.valid + `]}`
			value := tc.newValue()
			if err := json.Unmarshal([]byte(input), value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(encoded, &fields)
			var entries []json.RawMessage
			_ = json.Unmarshal(fields[tc.key], &entries)
			if len(entries) != 1 {
				t.Fatalf("valid entries: %s", encoded)
			}
		})
	}
}

func TestAlpha10ElicitationExactlyOneScope(t *testing.T) {
	for _, mode := range []string{"form", "url", "future"} {
		fields := `"mode":"` + mode + `","message":"Input","requestedSchema":{},"elicitationId":"e","url":"https://example.com"`
		for _, scope := range []string{``, `,"sessionId":"s","requestId":1`, `,"requestId":null`} {
			var request CreateElicitationRequest
			if json.Unmarshal([]byte(`{`+fields+scope+`}`), &request) == nil {
				t.Fatalf("accepted %s %s", mode, scope)
			}
		}
		for _, scope := range []string{`,"sessionId":"s"`, `,"requestId":9007199254740993`} {
			var request CreateElicitationRequest
			if err := json.Unmarshal([]byte(`{`+fields+scope+`}`), &request); err != nil {
				t.Fatalf("%s %s: %v", mode, scope, err)
			}
		}
	}
	for _, input := range []string{`{"sessionId":"s","requestId":1,"requestedSchema":{}}`, `{"sessionId":"s","requestedSchema":{"type":"array"}}`, `{"sessionId":"s","requestedSchema":{"type":null}}`} {
		var mode ElicitationFormMode
		if json.Unmarshal([]byte(input), &mode) == nil {
			t.Fatalf("accepted form %s", input)
		}
	}
	var mode ElicitationFormMode
	if err := json.Unmarshal([]byte(`{"requestId":"r","requestedSchema":{}}`), &mode); err != nil {
		t.Fatal(err)
	}
	// Constructor helpers must include the shared fields introduced by oneOf.
	id := RequestIdStr("r")
	form := NewElicitationFormModeRequest(RequestId{Str: &id}, ElicitationSchema{})
	if err := form.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAlpha10StrictListedSessionFields(t *testing.T) {

	for _, tc := range []struct {
		input string
		value any
	}{
		{`{"sessions":[],"nextCursor":42}`, &ListSessionsResponse{}},
		{`{"sessionId":"s","cwd":"/tmp","additionalDirectories":[null]}`, &SessionInfo{}},
		{`{"sessionId":"s","cwd":"/tmp","additionalDirectories":["/valid",42]}`, &SessionInfo{}},
	} {
		if json.Unmarshal([]byte(tc.input), tc.value) == nil {
			t.Fatalf("accepted %s", tc.input)
		}
	}
	for _, dirs := range []string{`null`, `[]`} {
		var info SessionInfo
		if err := json.Unmarshal([]byte(`{"sessionId":"s","cwd":"/tmp","additionalDirectories":`+dirs+`}`), &info); err != nil {
			t.Fatal(err)
		}
	}
	var response ListSessionsResponse
	if err := json.Unmarshal([]byte(`{"sessions":[{"sessionId":"bad","cwd":"/tmp","additionalDirectories":[null]},{"sessionId":"good","cwd":"/tmp"}]}`), &response); err != nil || len(response.Sessions) != 1 || response.Sessions[0].SessionId != "good" {
		t.Fatalf("session recovery: %+v %v", response, err)
	}

}

func TestAlpha10TerminalAndIntegerFields(t *testing.T) {
	for _, tc := range []struct {
		size  string
		state NullableFieldState
	}{
		{``, NullableFieldAbsent}, {`,"size":null`, NullableFieldNull}, {`,"size":{"cols":80,"rows":24}`, NullableFieldValue},
		{`,"size":{"cols":0,"rows":24}`, NullableFieldAbsent}, {`,"size":{"cols":65536,"rows":24}`, NullableFieldAbsent},
	} {
		var update TerminalUpdate
		if err := json.Unmarshal([]byte(`{"terminalId":"t"`+tc.size+`}`), &update); err != nil {
			t.Fatal(err)
		}
		if update.SizeState() != tc.state {
			t.Fatalf("size %s: %s", tc.size, update.SizeState())
		}
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		var again TerminalUpdate
		if err := json.Unmarshal(encoded, &again); err != nil || again.SizeState() != tc.state {
			t.Fatalf("roundtrip %s: %v", encoded, err)
		}
	}
	var exit TerminalExitStatus
	if err := json.Unmarshal([]byte(`{"exitCode":-9223372036854775808}`), &exit); err != nil || exit.ExitCode == nil || *exit.ExitCode != -9223372036854775808 {
		t.Fatalf("exit: %+v %v", exit, err)
	}
	var resource ResourceLink
	if err := json.Unmarshal([]byte(`{"type":"resource_link","name":"r","uri":"file:///r","size":18446744073709551615}`), &resource); err != nil || resource.Size == nil || *resource.Size != ^uint64(0) {
		t.Fatalf("size: %+v %v", resource, err)
	}
	if err := json.Unmarshal([]byte(`{"type":"resource_link","name":"r","uri":"file:///r","size":-1}`), &resource); err != nil || resource.Size != nil {
		t.Fatalf("negative size: %+v %v", resource, err)
	}
	var location ToolCallLocation
	if err := json.Unmarshal([]byte(`{"path":"/tmp","line":0}`), &location); err != nil || location.Line != nil {
		t.Fatalf("zero line: %+v %v", location, err)
	}
}

func TestAlpha10AddedMetadata(t *testing.T) {
	for _, input := range []struct {
		wire  string
		value any
	}{
		{`{"format":"git_patch","text":"diff","_meta":{"n":9007199254740993}}`, &DiffPatch{}},
		{`{"src":"file:///icon","_meta":{"n":9007199254740993}}`, &Icon{}},
		{`{"toolCall":{"toolCallId":"t"},"_meta":{"n":9007199254740993}}`, &ToolCallPermissionSubject{}},
		{`{"outcome":"cancelled","_meta":{"n":9007199254740993}}`, &RequestPermissionOutcome{}},
	} {
		if err := json.Unmarshal([]byte(input.wire), input.value); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(input.value)
		if err != nil || !jsonEqualForTest(raw, []byte(input.wire)) {
			t.Fatalf("metadata %s: %v", raw, err)
		}
	}
}

func TestAlpha10V2RequestIDsOverConnection(t *testing.T) {
	for _, clientRole := range []bool{false, true} {
		t.Run(fmt.Sprint(clientRole), func(t *testing.T) {
			a, b := net.Pipe()
			defer func() { _ = b.Close() }()
			if clientRole {
				c, err := NewClientSideConnection(&loopbackClient{}, a, a)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = c.Close() }()
			} else {
				c, err := NewAgentSideConnection(&alpha9Agent{}, a, a)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = c.Close() }()
			}
			_ = b.SetDeadline(time.Now().Add(5 * time.Second))
			reader := bufio.NewReader(b)
			for _, id := range []string{`null`, `1.5`, `true`, `9223372036854775808`, `"ok"`, `9007199254740993`} {
				if _, err := fmt.Fprintf(b, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"_probe\",\"params\":{}}\n", id); err != nil {
					t.Fatal(err)
				}
				frame, err := reader.ReadBytes('\n')
				if err != nil {
					t.Fatal(err)
				}
				var response struct {
					ID    json.RawMessage   `json:"id"`
					Error *acp.RequestError `json:"error"`
				}
				if err := json.Unmarshal(frame, &response); err != nil {
					t.Fatal(err)
				}
				want := -32600
				if id == `"ok"` || id == `9007199254740993` {
					want = -32601
				}
				if response.Error == nil || response.Error.Code != want || string(response.ID) != id {
					t.Fatalf("id %s: %s", id, frame)
				}
			}
		})
	}
}

func TestAlpha10InitializeNegotiationAndRepeat(t *testing.T) {
	for _, requested := range []int{0, 1} {
		a, b := net.Pipe()
		agent, err := NewAgentSideConnection(&alpha9Agent{}, a, a)
		if err != nil {
			t.Fatal(err)
		}
		peer := acp.NewConnection(nil, b, b)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = acp.SendRequest[InitializeResponse](peer, ctx, AgentMethodInitialize, json.RawMessage(fmt.Sprintf(`{"protocolVersion":%d}`, requested)))
		var reqErr *acp.RequestError
		if !errors.As(err, &reqErr) || reqErr.Code != -32022 {
			t.Fatalf("version %d before info: %v", requested, err)
		}
		data, _ := json.Marshal(reqErr.Data)
		if !jsonEqualForTest(data, []byte(fmt.Sprintf(`{"requested":%d,"supported":[2]}`, requested))) {
			t.Fatalf("data %s", data)
		}
		cancel()
		_ = peer.Close()
		_ = agent.Close()
	}
	for _, routed := range []bool{false, true} {
		t.Run(fmt.Sprint(routed), func(t *testing.T) {
			a, b := net.Pipe()
			var closeAgent func()
			if routed {
				router := &ProtocolRouter{V2: func(_ context.Context, _ string, raw json.RawMessage) (any, *acp.RequestError) {
					var req InitializeRequest
					if err := json.Unmarshal(raw, &req); err != nil {
						return nil, acp.NewInvalidParams(nil)
					}
					return InitializeResponse{ProtocolVersion: 2, Info: req.Info}, nil
				}}
				c := acp.NewConnection(router.Handle, a, a)
				closeAgent = func() { _ = c.Close() }
			} else {
				c, err := NewAgentSideConnection(&alpha9Agent{}, a, a)
				if err != nil {
					t.Fatal(err)
				}
				closeAgent = func() { _ = c.Close() }
			}
			defer closeAgent()
			peer := acp.NewConnection(nil, b, b)
			defer func() { _ = peer.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, params := range []string{`{"protocolVersion":3,"info":42,"capabilities":false}`, `{"protocolVersion":2,"info":{"name":"client","version":"0"}}`} {
				response, err := acp.SendRequest[InitializeResponse](peer, ctx, AgentMethodInitialize, json.RawMessage(params))
				if err != nil || response.ProtocolVersion != 2 {
					t.Fatalf("initialize %s: %+v %v", params, response, err)
				}
			}
			_, _ = acp.SendRequest[json.RawMessage](peer, ctx, "_other", json.RawMessage(`{}`))
			_, err := acp.SendRequest[InitializeResponse](peer, ctx, AgentMethodInitialize, json.RawMessage(`{"protocolVersion":2,"info":{"name":"client","version":"0"}}`))
			var reqErr *acp.RequestError
			if !errors.As(err, &reqErr) || reqErr.Code != -32600 {
				t.Fatalf("late repeat: %v", err)
			}
		})
	}
	empty := ProtocolRouter{}
	_, err := empty.Handle(context.Background(), AgentMethodInitialize, json.RawMessage(`{"protocolVersion":2}`))
	data, _ := json.Marshal(err.Data)
	if err.Code != -32022 || !reflect.DeepEqual(string(data), `{"requested":2,"supported":[]}`) {
		t.Fatalf("empty router: %v", err)
	}
}

func TestAlpha10EnvelopeIDsKeepIntegerPrecision(t *testing.T) {
	for _, id := range []string{`9007199254740993`, `9223372036854775807`, `-9223372036854775808`, `"request"`} {
		for _, tc := range []struct {
			input string
			value any
		}{
			{`{"id":` + id + `,"method":"_probe"}`, &AgentRequest{}},
			{`{"id":` + id + `,"method":"_probe"}`, &ClientRequest{}},
			{`{"id":` + id + `,"error":{"code":-32600,"message":"Invalid request"}}`, &AgentError{}},
			{`{"id":` + id + `,"error":{"code":-32600,"message":"Invalid request"}}`, &ClientError{}},
		} {
			if err := json.Unmarshal([]byte(tc.input), tc.value); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.value)
			if err != nil || !jsonEqualForTest(raw, []byte(tc.input)) {
				t.Fatalf("envelope %s: %v", raw, err)
			}
		}
	}
}
