package v2

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

type loopbackAgent struct {
	conn       *AgentSideConnection
	echoBefore bool
}

func (a *loopbackAgent) LoginAuth(context.Context, LoginAuthRequest) (LoginAuthResponse, error) {
	return LoginAuthResponse{}, nil
}
func (a *loopbackAgent) LogoutAuth(context.Context, LogoutAuthRequest) (LogoutAuthResponse, error) {
	return LogoutAuthResponse{}, nil
}
func (a *loopbackAgent) Initialize(_ context.Context, params InitializeRequest) (InitializeResponse, error) {
	return InitializeResponse{
		ProtocolVersion: ProtocolVersionNumber,
		Info:            Implementation{Name: "go-v2-test", Version: "0.0.0"},
	}, nil
}
func (a *loopbackAgent) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{SessionId: "sess-1"}, nil
}
func (a *loopbackAgent) Prompt(ctx context.Context, params PromptRequest) (PromptResponse, error) {
	const messageID MessageId = "message-1"
	echo := func(ctx context.Context) error {
		return a.conn.SessionUpdate(ctx, UpdateSessionNotification{
			SessionId: params.SessionId,
			Update: SessionUpdate{UserMessage: &SessionUpdateUserMessage{
				MessageId: messageID,
				Content:   params.Prompt,
			}},
		})
	}
	if a.echoBefore {
		if err := echo(ctx); err != nil {
			return PromptResponse{}, err
		}
	}
	if err := acp.AfterResponse(ctx, func(ctx context.Context) error {
		if !a.echoBefore {
			if err := echo(ctx); err != nil {
				return err
			}
		}
		if err := a.conn.SessionUpdate(ctx, UpdateSessionNotification{
			SessionId: params.SessionId,
			Update:    RunningUpdate(),
		}); err != nil {
			return err
		}
		return a.conn.SessionUpdate(ctx, UpdateSessionNotification{
			SessionId: params.SessionId,
			Update:    IdleUpdate(StopReasonEndTurn),
		})
	}); err != nil {
		return PromptResponse{}, err
	}
	return PromptResponse{MessageId: messageID}, nil
}
func (a *loopbackAgent) Cancel(context.Context, CancelSessionNotification) error { return nil }

type loopbackClient struct {
	mu      sync.Mutex
	updates []SessionUpdate
	idle    chan struct{}
}

func (c *loopbackClient) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}
func (c *loopbackClient) SessionUpdate(_ context.Context, params UpdateSessionNotification) error {
	c.mu.Lock()
	c.updates = append(c.updates, params.Update)
	n := len(c.updates)
	c.mu.Unlock()
	if n == 3 {
		select {
		case <-c.idle:
		default:
			close(c.idle)
		}
	}
	return nil
}

func TestV2PromptInsertionAndStateUpdates(t *testing.T) {
	t.Parallel()
	t.Run("echo-before-response", func(t *testing.T) { testV2PromptInsertion(t, true) })
	t.Run("echo-after-response", func(t *testing.T) { testV2PromptInsertion(t, false) })
}

func testV2PromptInsertion(t *testing.T, echoBefore bool) {
	t.Helper()
	agentSide, clientSide := net.Pipe()
	agentImpl := &loopbackAgent{echoBefore: echoBefore}
	agent, err := NewAgentSideConnection(agentImpl, agentSide, agentSide)
	if err != nil {
		t.Fatal(err)
	}
	agentImpl.conn = agent
	defer func() { _ = agent.Close() }()

	clientImpl := &loopbackClient{idle: make(chan struct{})}
	client, err := NewClientSideConnection(clientImpl, clientSide, clientSide)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	init, err := client.Initialize(ctx, InitializeRequest{
		ProtocolVersion: ProtocolVersionNumber,
		Info:            Implementation{Name: "go-v2-client", Version: "0.0.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != ProtocolVersionNumber {
		t.Fatalf("protocol version = %d", init.ProtocolVersion)
	}

	session, err := client.NewSession(ctx, NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := client.Prompt(ctx, PromptRequest{
		SessionId: session.SessionId,
		Prompt:    []ContentBlock{TextBlock("hello")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ack.MessageId != "message-1" {
		t.Fatalf("prompt response message ID = %q", ack.MessageId)
	}

	select {
	case <-clientImpl.idle:
	case <-ctx.Done():
		t.Fatal("timed out waiting for idle state_update")
	}
	clientImpl.mu.Lock()
	defer clientImpl.mu.Unlock()
	if len(clientImpl.updates) != 3 {
		t.Fatalf("updates = %d, want 3", len(clientImpl.updates))
	}
	message := clientImpl.updates[0].UserMessage
	if message == nil || message.MessageId != ack.MessageId || len(message.Content) != 1 || message.Content[0].Text == nil || message.Content[0].Text.Text != "hello" {
		t.Fatalf("user message = %#v, want echoed prompt with message ID %q", message, ack.MessageId)
	}
	if clientImpl.updates[1].StateUpdate == nil || *clientImpl.updates[1].StateUpdate.State != "running" {
		t.Fatalf("second update = %#v", clientImpl.updates[1].StateUpdate)
	}
	if clientImpl.updates[2].StateUpdate == nil || *clientImpl.updates[2].StateUpdate.State != "idle" {
		t.Fatalf("third update = %#v", clientImpl.updates[2].StateUpdate)
	}
	if clientImpl.updates[2].StateUpdate.StopReason == nil || *clientImpl.updates[2].StateUpdate.StopReason != StopReasonEndTurn {
		t.Fatalf("idle stopReason = %#v", clientImpl.updates[2].StateUpdate.StopReason)
	}
}

func TestV2RejectsNonV2Initialize(t *testing.T) {
	t.Parallel()
	agentSide, clientSide := net.Pipe()
	agent, err := NewAgentSideConnection(&loopbackAgent{}, agentSide, agentSide)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	client, err := NewClientSideConnection(&loopbackClient{idle: make(chan struct{})}, clientSide, clientSide)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.Initialize(ctx, InitializeRequest{
		ProtocolVersion: 1,
		Info:            Implementation{Name: "go-v1-client", Version: "0.0.0"},
	})
	if err == nil {
		t.Fatal("expected initialize to reject protocol version 1")
	}
	var reqErr *acp.RequestError
	if !errors.As(err, &reqErr) || reqErr.Code != -32602 {
		t.Fatalf("error = %v, want invalid params", err)
	}
}

func TestProtocolRouterSelectsV2(t *testing.T) {
	t.Parallel()
	router := &ProtocolRouter{
		V2: func(_ context.Context, method string, _ json.RawMessage) (any, *acp.RequestError) {
			if method != AgentMethodInitialize {
				t.Fatalf("method = %s", method)
			}
			return InitializeResponse{ProtocolVersion: ProtocolVersionNumber, Info: Implementation{Name: "r", Version: "0"}}, nil
		},
	}
	params, _ := json.Marshal(InitializeRequest{
		ProtocolVersion: ProtocolVersionNumber,
		Info:            Implementation{Name: "c", Version: "0"},
	})
	got, reqErr := router.Handle(context.Background(), AgentMethodInitialize, params)
	if reqErr != nil {
		t.Fatal(reqErr)
	}
	resp := got.(InitializeResponse)
	if resp.ProtocolVersion != ProtocolVersionNumber {
		t.Fatalf("version = %d", resp.ProtocolVersion)
	}
}

func TestStateUpdateJSONRoundTrip(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"sessionId":"s1","update":{"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn"}}`)
	var note UpdateSessionNotification
	if err := json.Unmarshal(raw, &note); err != nil {
		t.Fatal(err)
	}
	if note.Update.StateUpdate == nil || *note.Update.StateUpdate.State != "idle" {
		t.Fatalf("decoded %#v", note.Update.StateUpdate)
	}
	encoded, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	var again UpdateSessionNotification
	if err := json.Unmarshal(encoded, &again); err != nil {
		t.Fatal(err)
	}
	if again.Update.StateUpdate == nil || *again.Update.StateUpdate.StopReason != StopReasonEndTurn {
		t.Fatalf("round-trip %#v", again.Update.StateUpdate)
	}
}
