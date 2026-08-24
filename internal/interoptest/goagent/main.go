package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

type interopAgent struct {
	connection  *acp.AgentSideConnection
	nextSession atomic.Uint64
}

func (*interopAgent) Initialize(_ context.Context, request acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{
		AgentInfo: &acp.Implementation{
			Name:    "go-interop-agent",
			Version: "0.0.0",
		},
		ProtocolVersion: request.ProtocolVersion,
	}, nil
}

func (a *interopAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	id := a.nextSession.Add(1)
	return acp.NewSessionResponse{
		SessionId: acp.SessionId("go-session-" + strconv.FormatUint(id, 10)),
	}, nil
}

func (a *interopAgent) Prompt(ctx context.Context, request acp.PromptRequest) (acp.PromptResponse, error) {
	scenario, err := promptText(request.Prompt)
	if err != nil {
		return acp.PromptResponse{}, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}

	switch scenario {
	case "core":
		if err := a.update(ctx, request.SessionId, "core-1"); err != nil {
			return acp.PromptResponse{}, err
		}
		if err := a.update(ctx, request.SessionId, "core-2"); err != nil {
			return acp.PromptResponse{}, err
		}
		kind := acp.ToolKindExecute
		status := acp.ToolCallStatusPending
		title := "Interop permission"
		permission, err := a.connection.RequestPermission(ctx, acp.RequestPermissionRequest{
			SessionId: request.SessionId,
			ToolCall: acp.ToolCallUpdate{
				ToolCallId: "interop-tool",
				Title:      &title,
				Kind:       &kind,
				Status:     &status,
			},
			Options: []acp.PermissionOption{
				{OptionId: "allow", Name: "Allow", Kind: acp.PermissionOptionKindAllowOnce},
				{OptionId: "reject", Name: "Reject", Kind: acp.PermissionOptionKindRejectOnce},
			},
		})
		if err != nil {
			return acp.PromptResponse{}, err
		}
		if permission.Outcome.Selected == nil || permission.Outcome.Selected.OptionId != "allow" {
			return acp.PromptResponse{}, acp.NewInternalError(map[string]any{
				"error": "client did not select deterministic allow option",
			})
		}
		if err := a.update(ctx, request.SessionId, "core-3"); err != nil {
			return acp.PromptResponse{}, err
		}
		return acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, nil

	case "session-cancel":
		if err := a.update(ctx, request.SessionId, "session-cancel-ready"); err != nil {
			return acp.PromptResponse{}, err
		}
		<-ctx.Done()
		return acp.PromptResponse{StopReason: acp.StopReasonCancelled}, nil

	case "request-cancel":
		if err := a.update(ctx, request.SessionId, "request-cancel-ready"); err != nil {
			return acp.PromptResponse{}, err
		}
		<-ctx.Done()
		return acp.PromptResponse{}, context.Cause(ctx)

	default:
		return acp.PromptResponse{}, acp.NewInvalidParams(map[string]any{"scenario": scenario})
	}
}

func (*interopAgent) Cancel(context.Context, acp.CancelNotification) error {
	return nil
}

func (a *interopAgent) update(ctx context.Context, sessionID acp.SessionId, text string) error {
	return a.connection.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: sessionID,
		Update:    acp.UpdateAgentMessageText(text),
	})
}

func promptText(blocks []acp.ContentBlock) (string, error) {
	for _, block := range blocks {
		if block.Text != nil {
			return block.Text.Text, nil
		}
	}
	return "", fmt.Errorf("prompt must contain text")
}

func main() {
	implementation := &interopAgent{}
	connection, err := stdio.NewAgentConnection(implementation, acp.ConnectionOptions{})
	if err != nil {
		log.Fatal(err)
	}
	implementation.connection = connection
	defer func() { _ = connection.Close() }()

	err = connection.Wait(context.Background())
	if err != nil && !errors.Is(err, acp.ErrPeerClosed) && !errors.Is(err, acp.ErrConnectionClosed) {
		log.Print(err)
	}
}

var _ acp.Agent = (*interopAgent)(nil)
