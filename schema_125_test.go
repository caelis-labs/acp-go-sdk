package acp

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestCompactionPatchStatesAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		fields     string
		state      NullableFieldState
		summaryLen int
	}{
		{``, NullableFieldAbsent, 0},
		{`,"summary":null,"error":null,"_meta":null`, NullableFieldNull, 0},
		{`,"summary":[],"error":"","_meta":{}`, NullableFieldValue, 0},
		{`,"summary":[{"type":"text","text":"kept"},null,{"type":"text"}],"error":"failed","_meta":{"n":9007199254740993}`, NullableFieldValue, 1},
		{`,"summary":false,"error":42,"_meta":"invalid"`, NullableFieldAbsent, 0},
		{`,"summary":false,"error":123,"_meta":[]`, NullableFieldAbsent, 0},
	} {
		t.Run(tc.fields, func(t *testing.T) {
			input := `{"compactionId":"c1","status":"_future"` + tc.fields + `}`
			var standalone CompactionUpdate
			if err := json.Unmarshal([]byte(input), &standalone); err != nil {
				t.Fatal(err)
			}
			var update SessionUpdate
			if err := json.Unmarshal([]byte(`{"sessionUpdate":"compaction_update",`+input[1:]), &update); err != nil {
				t.Fatal(err)
			}
			for _, states := range [][3]NullableFieldState{
				{standalone.SummaryState(), standalone.ErrorState(), standalone.MetaState()},
				{update.CompactionUpdate.SummaryState(), update.CompactionUpdate.ErrorState(), update.CompactionUpdate.MetaState()},
			} {
				if states != [3]NullableFieldState{tc.state, tc.state, tc.state} {
					t.Fatalf("states=%v want %v", states, tc.state)
				}
			}
			if len(standalone.Summary) != tc.summaryLen {
				t.Fatalf("summary=%+v", standalone.Summary)
			}
			encoded, err := json.Marshal(update)
			if err != nil {
				t.Fatal(err)
			}
			var again SessionUpdate
			if err := json.Unmarshal(encoded, &again); err != nil {
				t.Fatal(err)
			}
			if again.CompactionUpdate.SummaryState() != tc.state {
				t.Fatalf("lost patch state: %s", encoded)
			}
			if tc.summaryLen == 1 && string(again.CompactionUpdate.Meta["n"]) != "9007199254740993" {
				t.Fatalf("metadata lost precision: %s", encoded)
			}
		})
	}
	var patch SessionCompactionUpdate
	patch.SetSummary([]ContentBlock{})
	patch.SetError("")
	patch.SetMeta(map[string]json.RawMessage{})
	if patch.SummaryState() != NullableFieldValue || patch.MetaState() != NullableFieldValue {
		t.Fatal("empty replacements lost")
	}
	patch.ClearSummary()
	patch.ClearError()
	patch.ClearMeta()
	if patch.SummaryState() != NullableFieldNull || patch.ErrorState() != NullableFieldNull || patch.MetaState() != NullableFieldNull {
		t.Fatal("clear lost")
	}
	patch.UnsetSummary()
	patch.UnsetError()
	patch.UnsetMeta()
	if patch.SummaryState() != NullableFieldAbsent || patch.ErrorState() != NullableFieldAbsent || patch.MetaState() != NullableFieldAbsent {
		t.Fatal("unset lost")
	}
}

func TestNoticeAndSessionCapabilities(t *testing.T) {
	for _, input := range []string{
		`{"sessionUpdate":"notice","severity":"_custom","title":"Title"}`,
		`{"sessionUpdate":"notice","severity":"future","title":"Title","description":123,"_meta":false}`,
	} {
		var update SessionUpdate
		if err := json.Unmarshal([]byte(input), &update); err != nil {
			t.Fatal(err)
		}
		if update.Notice == nil || update.Notice.Description != nil {
			t.Fatalf("notice=%+v", update)
		}
		if err := update.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{
		`{"sessionUpdate":"notice","severity":"info","title":""}`,
		`{"sessionUpdate":"notice","severity":null,"title":"Title"}`,
		`{"sessionUpdate":"compaction_update","status":"completed"}`,
		`{"sessionUpdate":"compaction_summary_chunk","compactionId":"c1","content":{"type":"text"}}`,
	} {
		var update SessionUpdate
		if json.Unmarshal([]byte(input), &update) == nil {
			t.Fatalf("accepted malformed update: %s", input)
		}
	}
	for _, input := range []string{`{}`, `{"compaction":null,"notices":null}`, `{"compaction":123,"notices":[]}`} {
		var caps ClientSessionCapabilities
		if err := json.Unmarshal([]byte(input), &caps); err != nil {
			t.Fatal(err)
		}
		if caps.Compaction != nil || caps.Notices != nil {
			t.Fatalf("advertised unsupported capability: %s", input)
		}
	}
	var caps ClientSessionCapabilities
	if err := json.Unmarshal([]byte(`{"compaction":{},"notices":{}}`), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Compaction == nil || caps.Notices == nil {
		t.Fatal("lost advertised support")
	}
}

func TestStrictActionInputsAndNullCollections(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		newValue    func() any
	}{
		{"terminal args number", `{"sessionId":"s","command":"run","args":["--port",3000]}`, func() any { return &CreateTerminalRequest{} }},
		{"terminal args null", `{"sessionId":"s","command":"run","args":[null]}`, func() any { return &CreateTerminalRequest{} }},
		{"terminal env", `{"sessionId":"s","command":"run","env":[{"name":"X","value":null}]}`, func() any { return &CreateTerminalRequest{} }},
		{"terminal cwd", `{"sessionId":"s","command":"run","cwd":123}`, func() any { return &CreateTerminalRequest{} }},
		{"terminal output limit", `{"sessionId":"s","command":"run","outputByteLimit":"10"}`, func() any { return &CreateTerminalRequest{} }},
		{"file line", `{"sessionId":"s","path":"/file","line":"120"}`, func() any { return &ReadTextFileRequest{} }},
		{"file limit", `{"sessionId":"s","path":"/file","limit":false}`, func() any { return &ReadTextFileRequest{} }},
		{"session invalid server", `{"cwd":"/","mcpServers":[{"name":"m","command":"/m","args":[],"env":[{"name":"X","value":null}]}]}`, func() any { return &NewSessionRequest{} }},
		{"session unknown server", `{"cwd":"/","mcpServers":[{"type":"future","name":"m","url":"https://m","headers":[]}]}`, func() any { return &NewSessionRequest{} }},
		{"load invalid server", `{"sessionId":"s","cwd":"/","mcpServers":[null]}`, func() any { return &LoadSessionRequest{} }},
		{"resume invalid server", `{"sessionId":"s","cwd":"/","mcpServers":false}`, func() any { return &ResumeSessionRequest{} }},
		{"terminal auth env null", `{"type":"terminal","id":"auth","name":"Auth","env":{"X":null}}`, func() any { return &AuthMethod{} }},
		{"terminal auth env number", `{"type":"terminal","id":"auth","name":"Auth","env":{"X":1}}`, func() any { return &AuthMethod{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if json.Unmarshal([]byte(tc.input), tc.newValue()) == nil {
				t.Fatalf("accepted %s", tc.input)
			}
		})
	}
	var request NewSessionRequest
	if err := json.Unmarshal([]byte(`{"cwd":"/","mcpServers":null}`), &request); err != nil {
		t.Fatal(err)
	}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"cwd":"/","mcpServers":[{"name":"m","command":"/m","args":null,"env":null}]}`), &request); err != nil {
		t.Fatal(err)
	}
	if len(request.McpServers) != 1 || request.McpServers[0].Stdio == nil {
		t.Fatal("null collections dropped server")
	}
	var terminal CreateTerminalRequest
	if err := json.Unmarshal([]byte(`{"sessionId":"s","command":"run","args":null,"env":null}`), &terminal); err != nil {
		t.Fatal(err)
	}
}

type schema125Client struct{ updates chan SessionUpdate }

func (*schema125Client) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}
func (c *schema125Client) SessionUpdate(_ context.Context, n SessionNotification) error {
	c.updates <- n.Update
	return nil
}

func TestSchema125OrderedTypedLoopback(t *testing.T) {
	a, b := net.Pipe()
	receiver := &schema125Client{updates: make(chan SessionUpdate, 4)}
	agent, err := NewAgentSideConnectionWithOptions(minimalAgent{}, a, a, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	client, err := NewClientSideConnectionWithOptions(receiver, b, b, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	updates := []SessionUpdate{
		{Notice: &SessionUpdateNotice{Severity: NoticeSeverityWarning, Title: "Notice"}},
		{CompactionUpdate: &SessionCompactionUpdate{CompactionId: "c", Status: CompactionStatusInProgress}},
		{CompactionSummaryChunk: &SessionUpdateCompactionSummaryChunk{CompactionId: "c", Content: TextBlock("summary")}},
		{CompactionUpdate: &SessionCompactionUpdate{CompactionId: "c", Status: CompactionStatusCompleted}},
	}
	updates[3].CompactionUpdate.ClearError()
	updates[3].CompactionUpdate.SetSummary([]ContentBlock{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, update := range updates {
		if err := agent.SessionUpdate(ctx, SessionNotification{SessionId: "s", Update: update}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range updates {
		select {
		case got := <-receiver.updates:
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if !reflect.DeepEqual(wantJSON, gotJSON) {
				t.Fatalf("got %s want %s", gotJSON, wantJSON)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestInitializeDropsMalformedTerminalAuthMethod(t *testing.T) {
	var response InitializeResponse
	input := `{"protocolVersion":1,"authMethods":[{"type":"terminal","id":"bad","name":"Bad","args":["--port",8123]},{"type":"terminal","id":"bad-env","name":"Bad","env":{"X":null}},{"id":"good","name":"Good"}]}`
	if err := json.Unmarshal([]byte(input), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.AuthMethods) != 1 || response.AuthMethods[0].Agent == nil || response.AuthMethods[0].Agent.Id != "good" {
		t.Fatalf("auth methods: %+v", response.AuthMethods)
	}
}

type strictActionClient struct{ calls int }

func (*strictActionClient) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}
func (*strictActionClient) SessionUpdate(context.Context, SessionNotification) error { return nil }
func (c *strictActionClient) CreateTerminal(context.Context, CreateTerminalRequest) (CreateTerminalResponse, error) {
	c.calls++
	return CreateTerminalResponse{TerminalId: "t"}, nil
}
func (*strictActionClient) TerminalOutput(context.Context, TerminalOutputRequest) (TerminalOutputResponse, error) {
	return TerminalOutputResponse{}, nil
}
func (*strictActionClient) ReleaseTerminal(context.Context, ReleaseTerminalRequest) (ReleaseTerminalResponse, error) {
	return ReleaseTerminalResponse{}, nil
}
func (*strictActionClient) WaitForTerminalExit(context.Context, WaitForTerminalExitRequest) (WaitForTerminalExitResponse, error) {
	return WaitForTerminalExitResponse{}, nil
}
func (*strictActionClient) KillTerminal(context.Context, KillTerminalRequest) (KillTerminalResponse, error) {
	return KillTerminalResponse{}, nil
}

func TestStrictTerminalDispatchRejectsBeforeHandler(t *testing.T) {
	impl := &strictActionClient{}
	client := &ClientSideConnection{client: impl}
	for _, input := range []string{`{"sessionId":"s","command":"run","args":[null]}`, `{"sessionId":"s","command":"run","env":[{"name":"X","value":null}]}`} {
		params := json.RawMessage(input)
		if _, err := client.handle(testInboundContext(InboundRequest, params), ClientMethodTerminalCreate, params); err == nil || err.Code != -32602 {
			t.Fatalf("error=%v want invalid params", err)
		}
	}
	if impl.calls != 0 {
		t.Fatal("malformed action reached handler")
	}
	params := json.RawMessage(`{"sessionId":"s","command":"run","args":null,"env":null}`)
	if _, err := client.handle(testInboundContext(InboundRequest, params), ClientMethodTerminalCreate, params); err != nil {
		t.Fatal(err)
	}
	if impl.calls != 1 {
		t.Fatal("valid null collections did not reach handler")
	}
}

func TestCompactionRecoveryNotification(t *testing.T) {
	a, b := net.Pipe()
	receiver := &schema125Client{updates: make(chan SessionUpdate, 1)}
	client, err := NewClientSideConnectionWithOptions(receiver, b, b, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	peer := NewConnection(nil, a, a)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		fields string
		states [3]NullableFieldState
	}{
		{`"summary":[{"type":"text","text":"retained"}],"error":"retained","_meta":{"n":9007199254740993}`, [3]NullableFieldState{NullableFieldValue, NullableFieldValue, NullableFieldValue}},
		{`"summary":false,"error":42,"_meta":"invalid"`, [3]NullableFieldState{NullableFieldAbsent, NullableFieldAbsent, NullableFieldAbsent}},
		{`"summary":false,"error":null,"_meta":{}`, [3]NullableFieldState{NullableFieldAbsent, NullableFieldNull, NullableFieldValue}},
		{`"summary":null,"error":null,"_meta":null`, [3]NullableFieldState{NullableFieldNull, NullableFieldNull, NullableFieldNull}},
	} {
		wire := json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"compaction_update","compactionId":"c","status":"failed",` + tc.fields + `}}`)
		if err := peer.SendNotification(ctx, ClientMethodSessionUpdate, wire); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-receiver.updates:
			patch := got.CompactionUpdate
			if patch == nil {
				t.Fatal("compaction update not dispatched")
			}
			states := [3]NullableFieldState{patch.SummaryState(), patch.ErrorState(), patch.MetaState()}
			if states != tc.states {
				t.Fatalf("patch states=%v want=%v; input=%s", states, tc.states, wire)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for i, key := range []string{"summary", "error", "_meta"} {
				raw, present := fields[key]
				switch tc.states[i] {
				case NullableFieldAbsent:
					if present {
						t.Fatalf("malformed %s became a patch: %s", key, encoded)
					}
				case NullableFieldNull:
					if string(raw) != "null" {
						t.Fatalf("explicit clear lost for %s: %s", key, encoded)
					}
				case NullableFieldValue:
					if !present || string(raw) == "null" {
						t.Fatalf("replacement lost for %s: %s", key, encoded)
					}
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
