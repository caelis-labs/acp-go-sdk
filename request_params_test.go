package acp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

type emptyParamsAgent struct{ minimalAgent }

func (emptyParamsAgent) ListSessions(context.Context, ListSessionsRequest) (ListSessionsResponse, error) {
	return ListSessionsResponse{Sessions: []SessionInfo{}}, nil
}
func (emptyParamsAgent) Logout(context.Context, LogoutRequest) (LogoutResponse, error) {
	return LogoutResponse{}, nil
}

func TestRequestEmptyPayloadsStillValidateRequiredFields(t *testing.T) {
	isInvalidParams := func(err error) bool {
		var reqErr *RequestError
		return errors.As(err, &reqErr) && reqErr.Code == -32602
	}
	a, b := net.Pipe()
	agent := NewAgentSideConnection(emptyParamsAgent{}, a, a)
	defer func() { _ = agent.Close() }()
	peer := NewConnection(nil, b, b)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, params := range []any{nil, json.RawMessage(`null`), json.RawMessage(`{}`)} {
		for _, method := range []string{AgentMethodSessionList, AgentMethodLogout} {
			if _, err := SendRequest[json.RawMessage](peer, ctx, method, params); err != nil {
				t.Fatalf("%s(%s): %v", method, params, err)
			}
		}
		for _, method := range []string{AgentMethodInitialize, AgentMethodSessionNew, AgentMethodSessionPrompt} {
			_, err := SendRequest[json.RawMessage](peer, ctx, method, params)
			if !isInvalidParams(err) {
				t.Fatalf("%s(%s): %v, want invalid params", method, params, err)
			}
		}
	}
	for _, params := range []json.RawMessage{json.RawMessage(`[]`), json.RawMessage(`false`), json.RawMessage(`1`), json.RawMessage(`""`)} {
		if _, err := SendRequest[json.RawMessage](peer, ctx, AgentMethodSessionList, params); !isInvalidParams(err) {
			t.Fatalf("list(%s): %v, want invalid params", params, err)
		}
	}
}
