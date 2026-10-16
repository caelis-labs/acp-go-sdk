package v2

import (
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

type alpha9Agent struct{ loopbackAgent }

func (*alpha9Agent) Initialize(_ context.Context, req InitializeRequest) (InitializeResponse, error) {
	return InitializeResponse{ProtocolVersion: req.ProtocolVersion, Info: Implementation{Name: "agent", Version: "0"}}, nil
}

func (*alpha9Agent) ListSessions(context.Context, ListSessionsRequest) (ListSessionsResponse, error) {
	return ListSessionsResponse{Sessions: []SessionInfo{}}, nil
}

func TestAlpha9EmptyRequestsAndVersionNegotiation(t *testing.T) {
	a, b := net.Pipe()
	agent, err := NewAgentSideConnection(&alpha9Agent{}, a, a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	peer := acp.NewConnection(nil, b, b)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, version := range []int{2, 3, 65535} {
		response, err := acp.SendRequest[InitializeResponse](peer, ctx, AgentMethodInitialize,
			json.RawMessage(fmt.Sprintf(`{"protocolVersion":%d,"info":{"name":"client","version":"0"}}`, version)))
		if err != nil || response.ProtocolVersion != 2 {
			t.Fatalf("initialize(%d): %+v %v", version, response, err)
		}
	}
	for _, params := range []any{nil, json.RawMessage(`null`), json.RawMessage(`{}`)} {
		for _, method := range []string{AgentMethodSessionList, AgentMethodAuthLogout} {
			if _, err := acp.SendRequest[json.RawMessage](peer, ctx, method, params); err != nil {
				t.Fatalf("%s(%s): %v", method, params, err)
			}
		}
		for _, method := range []string{AgentMethodInitialize, AgentMethodSessionNew, AgentMethodSessionPrompt} {
			_, err := acp.SendRequest[json.RawMessage](peer, ctx, method, params)
			var reqErr *acp.RequestError
			if !errors.As(err, &reqErr) || reqErr.Code != -32602 {
				t.Fatalf("%s(%s): %v, want invalid params", method, params, err)
			}
		}
	}
}

func TestAlpha9RouterPreservesInitializePayload(t *testing.T) {
	for _, version := range []int{2, 3, 65535} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			input := json.RawMessage(fmt.Sprintf(`{"protocolVersion":%d,"info":{"name":"client","version":"0","future":{"n":9007199254740993}},"capabilities":{"session":{"future":{}},"_meta":{"n":9007199254740993},"future":{"nested":true}},"_meta":{"n":9007199254740993},"future":{"raw":[null,1.2500]}}`, version))
			var want map[string]json.RawMessage
			if err := json.Unmarshal(input, &want); err != nil {
				t.Fatal(err)
			}
			want["protocolVersion"] = json.RawMessage(`2`)
			calls := 0
			router := ProtocolRouter{V2: func(_ context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
				calls++
				if method == AgentMethodInitialize {
					var got map[string]json.RawMessage
					if err := json.Unmarshal(params, &got); err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("payload changed: %s (%v)", params, err)
					}
				}
				return InitializeResponse{ProtocolVersion: 2, Info: Implementation{Name: "agent", Version: "0"}}, nil
			}}
			if _, err := router.Handle(context.Background(), AgentMethodInitialize, input); err != nil {
				t.Fatal(err)
			}
			if _, err := router.Handle(context.Background(), AgentMethodSessionNew, json.RawMessage(`{}`)); err != nil || calls != 2 || router.selected != 2 {
				t.Fatalf("routing: selected=%d calls=%d err=%v", router.selected, calls, err)
			}
		})
	}
}

func TestAlpha9RouterV1FallbackOnlyMapsSharedCapabilities(t *testing.T) {
	for _, version := range []int{0, 2, 3, 65535} {
		called := false
		router := ProtocolRouter{V1: func(_ context.Context, _ string, params json.RawMessage) (any, *acp.RequestError) {
			called = true
			var request acp.InitializeRequest
			if err := json.Unmarshal(params, &request); err != nil {
				t.Fatal(err)
			}
			if request.ProtocolVersion != 1 || request.ClientInfo == nil || request.ClientInfo.Name != "client" {
				t.Fatalf("invalid fallback: %s", params)
			}
			if request.ClientCapabilities.Terminal || request.ClientCapabilities.Fs.ReadTextFile || request.ClientCapabilities.Fs.WriteTextFile {
				t.Fatalf("unknown capabilities enabled I/O: %s", params)
			}
			if version >= 2 {
				if !request.ClientCapabilities.Auth.Terminal || string(request.Meta["n"]) != "9007199254740993" || string(request.ClientCapabilities.Auth.Meta["n"]) != "9007199254740993" {
					t.Fatalf("shared initialize fields lost: %s", params)
				}
			}
			return acp.InitializeResponse{ProtocolVersion: 1}, nil
		}}
		var wire string
		if version == 0 {
			wire = `{"protocolVersion":0,"clientInfo":{"name":"client","version":"0"}}`
		} else {
			wire = fmt.Sprintf(`{"protocolVersion":%d,"info":{"name":"client","version":"0"},"capabilities":{"fs":{"readTextFile":true,"writeTextFile":true},"terminal":true,"auth":{"terminal":{},"_meta":{"n":9007199254740993}}},"_meta":{"n":9007199254740993}}`, version)
		}
		if _, err := router.Handle(context.Background(), AgentMethodInitialize, json.RawMessage(wire)); err != nil || !called {
			t.Fatalf("fallback(%d): called=%t err=%v", version, called, err)
		}
	}
}

func TestAlpha9MetadataPatchWireValues(t *testing.T) {
	for _, fields := range []string{
		`"sessionUpdate":"user_message","messageId":"m"`,
		`"sessionUpdate":"agent_message","messageId":"m"`,
		`"sessionUpdate":"agent_thought","messageId":"m"`,
		`"sessionUpdate":"tool_call_update","toolCallId":"t"`,
		`"sessionUpdate":"terminal_update","terminalId":"t"`,
		`"sessionUpdate":"session_info_update"`,
		`"sessionUpdate":"compaction_update","compactionId":"c","status":"completed"`,
	} {
		for _, meta := range []string{``, `,"_meta":null`, `,"_meta":{}`, `,"_meta":{"remove":null,"replace":{"n":9007199254740993,"empty":{}},"array":[]}`} {
			input := []byte(`{` + fields + meta + `}`)
			var update SessionUpdate
			if err := json.Unmarshal(input, &update); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(update)
			if err != nil {
				t.Fatal(err)
			}
			var want, got map[string]json.RawMessage
			if err := json.Unmarshal(input, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if (got["_meta"] == nil) != (want["_meta"] == nil) {
				t.Fatalf("metadata patch changed: %s -> %s", input, encoded)
			}
			if meta != "" {
				assertAlpha8RawFields(t, update, `"_meta":`+string(want["_meta"]))
			}
		}
	}
}

func TestAlpha9PlanContentAllowsFileAndMarkdownAsRawVariants(t *testing.T) {
	for _, kind := range []string{"file", "markdown", "future"} {
		wire := fmt.Sprintf(`{"type":%q,"planId":"p","payload":{"n":9007199254740993,"value":null}}`, kind)
		var content PlanUpdateContent
		if err := json.Unmarshal([]byte(wire), &content); err != nil {
			t.Fatal(err)
		}
		if content.Other == nil {
			t.Fatalf("lost raw plan content: %s", wire)
		}
		assertAlpha8RawFields(t, content, `"type":`+fmt.Sprintf("%q", kind)+`,"payload":{"n":9007199254740993,"value":null}`)
	}
}

func TestAlpha9UpsertRecoveryAndMutators(t *testing.T) {
	for _, tc := range []struct {
		input string
		state NullableFieldState
	}{
		{``, NullableFieldAbsent},
		{`,"content":null,"_meta":null`, NullableFieldNull},
		{`,"content":[],"_meta":{}`, NullableFieldValue},
		{`,"content":false,"_meta":123`, NullableFieldAbsent},
	} {
		var message UserMessage
		if err := json.Unmarshal([]byte(`{"messageId":"m"`+tc.input+`}`), &message); err != nil {
			t.Fatal(err)
		}
		if message.MetaState() != tc.state || message.ContentState() != tc.state {
			t.Fatalf("lost patch state for %s: meta=%s content=%s", tc.input, message.MetaState(), message.ContentState())
		}
		var update SessionUpdate
		encoded, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(`{"sessionUpdate":"user_message",`+string(encoded[1:])), &update); err != nil {
			t.Fatal(err)
		}
		if update.UserMessage.MetaState() != tc.state || update.UserMessage.ContentState() != tc.state {
			t.Fatalf("flattened update lost state: %s", encoded)
		}
	}
	terminal := TerminalUpdate{TerminalId: "t"}
	terminal.SetCwd(AbsolutePath("/tmp"))
	terminal.SetExitStatus(TerminalExitStatus{})
	if terminal.Cwd == nil || *terminal.Cwd != "/tmp" || terminal.ExitStatusState() != NullableFieldValue {
		t.Fatal("referenced patch value lost")
	}
	terminal.ClearCwd()
	terminal.UnsetExitStatus()
	if terminal.CwdState() != NullableFieldNull || terminal.ExitStatusState() != NullableFieldAbsent {
		t.Fatal("referenced patch presence lost")
	}
}

func TestAlpha9ClientRejectsUnsupportedInitializeResponse(t *testing.T) {
	for _, version := range []int{1, 3} {
		a, b := net.Pipe()
		peer := acp.NewConnection(func(context.Context, string, json.RawMessage) (any, *acp.RequestError) {
			return InitializeResponse{ProtocolVersion: ProtocolVersion(version), Info: Implementation{Name: "agent", Version: "0"}}, nil
		}, a, a)
		client, err := NewClientSideConnection(&loopbackClient{}, b, b)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = client.Initialize(ctx, InitializeRequest{ProtocolVersion: 2, Info: Implementation{Name: "client", Version: "0"}})
		cancel()
		_ = client.Close()
		_ = peer.Close()
		if err == nil {
			t.Fatalf("accepted selected protocol version %d", version)
		}
	}
}

type alpha9ActivityAgent struct {
	loopbackAgent
	finish <-chan struct{}
}

func (a *alpha9ActivityAgent) Prompt(ctx context.Context, req PromptRequest) (PromptResponse, error) {
	if err := a.conn.SessionUpdate(ctx, UpdateSessionNotification{SessionId: req.SessionId, Update: RunningUpdate()}); err != nil {
		return PromptResponse{}, err
	}
	if err := acp.AfterResponse(ctx, func(ctx context.Context) error {
		for _, wire := range []string{
			`{"sessionUpdate":"user_message","messageId":"accepted","content":[{"type":"text","text":"prompt"}]}`,
			`{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"partial"}}`,
			`{"sessionUpdate":"agent_message","messageId":"reply","content":[]}`,
			`{"sessionUpdate":"agent_message_chunk","messageId":"reply","content":{"type":"text","text":"later"}}`,
		} {
			var update SessionUpdate
			if err := json.Unmarshal([]byte(wire), &update); err != nil {
				return err
			}
			if err := a.conn.SessionUpdate(ctx, UpdateSessionNotification{SessionId: req.SessionId, Update: update}); err != nil {
				return err
			}
		}
		select {
		case <-a.finish:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Repeated idle snapshots are legal and remain observable. Applications
		// must not count each snapshot as a separate activity completion.
		for range 2 {
			if err := a.conn.SessionUpdate(ctx, UpdateSessionNotification{SessionId: req.SessionId, Update: IdleUpdate("end_turn")}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return PromptResponse{}, err
	}
	return PromptResponse{MessageId: "accepted"}, nil
}

type alpha9ActivityClient struct {
	Client
	updates chan SessionUpdate
}

func (c *alpha9ActivityClient) SessionUpdate(_ context.Context, req UpdateSessionNotification) error {
	c.updates <- req.Update
	return nil
}

func TestAlpha9PromptAckAndMessageUpsertsDoNotFinishActivity(t *testing.T) {
	a, b := net.Pipe()
	finish := make(chan struct{})
	agentImpl := &alpha9ActivityAgent{finish: finish}
	agent, err := NewAgentSideConnection(agentImpl, a, a)
	if err != nil {
		t.Fatal(err)
	}
	agentImpl.conn = agent
	defer func() { _ = agent.Close() }()
	receiver := &alpha9ActivityClient{updates: make(chan SessionUpdate, 8)}
	client, err := NewClientSideConnection(receiver, b, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ack, err := client.Prompt(ctx, PromptRequest{SessionId: "s", Prompt: []ContentBlock{TextBlock("prompt")}})
	if err != nil || ack.MessageId != "accepted" {
		t.Fatalf("insertion ack: %+v %v", ack, err)
	}
	next := func() SessionUpdate {
		select {
		case update := <-receiver.updates:
			return update
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return SessionUpdate{}
		}
	}
	if state := next().StateUpdate; state == nil || state.StateUpdate.Running == nil {
		t.Fatal("missing running activity")
	}
	if message := next().UserMessage; message == nil || message.MessageId != ack.MessageId {
		t.Fatal("missing inserted prompt echo")
	}
	if chunk := next().AgentMessageChunk; chunk == nil || chunk.MessageId != "reply" {
		t.Fatal("missing initial chunk")
	}
	if upsert := next().AgentMessage; upsert == nil || upsert.ContentState() != NullableFieldValue || len(upsert.Content) != 0 {
		t.Fatal("empty message replacement lost")
	}
	if chunk := next().AgentMessageChunk; chunk == nil || chunk.Content.Text.Text != "later" {
		t.Fatal("later chunk lost")
	}
	close(finish)
	for range 2 {
		if state := next().StateUpdate; state == nil || state.StateUpdate.Idle == nil || state.StateUpdate.Idle.IdleStateUpdate.EndTurn == nil {
			t.Fatal("missing explicit idle activity snapshot")
		}
	}
}
