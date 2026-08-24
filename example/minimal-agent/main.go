package main

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync/atomic"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

type agent struct {
	connection  *acp.AgentSideConnection
	nextSession atomic.Uint64
}

func (a *agent) Initialize(_ context.Context, _ acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		AgentInfo: &acp.Implementation{
			Name:    "minimal-go-agent",
			Title:   acp.Ptr("Minimal Go Agent"),
			Version: "0.0.0",
		},
		ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion),
	}, nil
}

func (a *agent) NewSession(_ context.Context, _ acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := a.nextSession.Add(1)
	return acp.NewSessionResponse{
		SessionId: acp.SessionId("session-" + strconv.FormatUint(id, 10)),
	}, nil
}

func (a *agent) Prompt(ctx context.Context, params acp.PromptRequest) (acp.PromptResponse, error) {
	err := a.connection.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: params.SessionId,
		Update:    acp.UpdateAgentMessageText("Hello from the minimal Go ACP agent."),
	})
	if err != nil {
		return acp.PromptResponse{}, err
	}
	return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil
}

func (*agent) Cancel(context.Context, acp.CancelNotification) error {
	return nil
}

func main() {
	implementation := &agent{}
	connection, err := stdio.NewAgentConnection(implementation, acp.ConnectionOptions{})
	if err != nil {
		log.Fatal(err)
	}
	implementation.connection = connection
	defer func() { _ = connection.Close() }()

	err = connection.Wait(context.Background())
	if err != nil && !errors.Is(err, acp.ErrPeerClosed) {
		log.Print(err)
	}
}

var _ acp.Agent = (*agent)(nil)
