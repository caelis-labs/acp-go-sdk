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

type errorAgent struct {
	Agent
	err error
}

func (a errorAgent) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{}, a.err
}

func TestDispatchPreservesProtocolErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want *acp.RequestError
	}{
		{"auth", acp.NewAuthRequired(map[string]any{"reason": "login"}), acp.NewAuthRequired(map[string]any{"reason": "login"})},
		{"wrapped", fmt.Errorf("handler: %w", acp.NewInvalidParams("detail")), acp.NewInvalidParams("detail")},
		{"cancel", context.Canceled, acp.NewRequestCancelled(map[string]any{"error": context.Canceled.Error()})},
		{"deadline", context.DeadlineExceeded, acp.NewRequestCancelled(map[string]any{"error": context.DeadlineExceeded.Error()})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			server, err := NewAgentSideConnection(errorAgent{err: tc.err}, a, a)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = server.Close() }()
			peer := acp.NewConnection(nil, b, b)
			defer func() { _ = peer.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err = acp.SendRequest[NewSessionResponse](peer, ctx, AgentMethodSessionNew, NewSessionRequest{Cwd: "/tmp"})
			var got *acp.RequestError
			if !errors.As(err, &got) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

// Obtain transport-owned metadata instead of reproducing private context keys.
func inboundContext(t *testing.T, notification bool) context.Context {
	t.Helper()
	a, b := net.Pipe()
	contexts := make(chan context.Context, 1)
	server := acp.NewConnection(func(ctx context.Context, _ string, _ json.RawMessage) (any, *acp.RequestError) {
		contexts <- ctx
		return nil, nil
	}, a, a)
	defer func() { _ = server.Close() }()
	peer := acp.NewConnection(nil, b, b)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if notification {
		if err := peer.SendNotification(ctx, "test", nil); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := acp.SendRequest[any](peer, ctx, "test", nil); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-contexts:
		return got
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return nil
	}
}

func TestDispatchRejectsWrongMessageKindsBeforeDecoding(t *testing.T) {
	requestCtx, notificationCtx := inboundContext(t, false), inboundContext(t, true)
	agent := &AgentSideConnection{} // A dispatched business method would panic.
	client := &ClientSideConnection{}
	for _, tc := range []struct {
		handler acp.MethodHandler
		methods []string
		ctx     context.Context
	}{
		{agent.handle, []string{AgentMethodSessionCancel}, requestCtx},
		{agent.handle, []string{AgentMethodInitialize, AgentMethodSessionNew, AgentMethodSessionPrompt, AgentMethodSessionResume, AgentMethodSessionList, AgentMethodSessionClose, AgentMethodSessionDelete, AgentMethodAuthLogin, AgentMethodAuthLogout, AgentMethodSessionSetConfigOption}, notificationCtx},
		{client.handle, []string{ClientMethodSessionUpdate, ClientMethodElicitationComplete}, requestCtx},
		{client.handle, []string{ClientMethodSessionRequestPermission, ClientMethodElicitationCreate}, notificationCtx},
	} {
		for _, method := range tc.methods {
			_, err := tc.handler(tc.ctx, method, json.RawMessage(`null`))
			if err == nil || err.Code != -32601 {
				t.Fatalf("%s: %v", method, err)
			}
		}
	}
}

type elicitationClient struct {
	Client
	completed chan CompleteElicitationNotification
}

func (c *elicitationClient) CreateElicitation(_ context.Context, req CreateElicitationRequest) (CreateElicitationResponse, error) {
	if req.Url == nil {
		return CreateElicitationResponse{}, errors.New("expected URL elicitation")
	}
	var resp CreateElicitationResponse
	err := json.Unmarshal([]byte(`{"action":"accept"}`), &resp)
	return resp, err
}
func (c *elicitationClient) CompleteElicitation(_ context.Context, req CompleteElicitationNotification) error {
	c.completed <- req
	return nil
}

func TestElicitationRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	impl := &elicitationClient{completed: make(chan CompleteElicitationNotification, 1)}
	client, err := NewClientSideConnection(impl, a, a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	agent, err := NewAgentSideConnection(nil, b, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var req CreateElicitationRequest
	if err := json.Unmarshal([]byte(`{"mode":"url","message":"Sign in","elicitationId":"e","url":"https://example.com","sessionId":"s"}`), &req); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.CreateElicitation(ctx, req); err != nil {
		t.Fatal(err)
	}
	if err := agent.CompleteElicitation(ctx, CompleteElicitationNotification{ElicitationId: "e"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-impl.completed:
		if got.ElicitationId != "e" {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
