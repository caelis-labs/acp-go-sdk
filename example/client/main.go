// Command client runs one prompt against an ACP agent executable over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

type client struct{}

func (client) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	// This example has no approval UI, so it never grants tool permission.
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeCancelled(),
	}, nil
}

func (client) SessionUpdate(_ context.Context, notification acp.SessionNotification) error {
	chunk := notification.Update.AgentMessageChunk
	if chunk != nil && chunk.Content.Text != nil {
		_, err := fmt.Print(chunk.Content.Text.Text)
		return err
	}
	return nil
}

func run(agentPath string, agentArgs []string, prompt string) (err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	process, err := stdio.StartClient(ctx, client{}, stdio.Command{
		Executable: agentPath,
		Args:       agentArgs,
	}, acp.ConnectionOptions{})
	if err != nil {
		return fmt.Errorf("start agent: %w", err)
	}
	defer func() {
		// Use a fresh deadline so cleanup still runs if the prompt timed out.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, process.Shutdown(shutdownCtx))
	}()

	initialized, err := process.Connection.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion),
		ClientInfo:      &acp.Implementation{Name: "example-go-client", Version: "0.0.0"},
		// No optional filesystem or terminal capabilities are advertised.
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if initialized.ProtocolVersion != acp.ProtocolVersion(acp.WireProtocolVersion) {
		return fmt.Errorf("agent selected unsupported ACP protocol version %d", initialized.ProtocolVersion)
	}

	session, err := process.Connection.NewSession(ctx, acp.NewSessionRequest{
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	response, err := process.Connection.Prompt(ctx, acp.PromptRequest{
		SessionId: session.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(prompt)},
	})
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	_, err = fmt.Printf("\nStop reason: %s\n", response.StopReason)
	return err
}

func main() {
	agentPath := flag.String("agent", "", "ACP agent executable (required)")
	prompt := flag.String("prompt", "Hello", "text prompt to send")
	flag.Parse()
	if *agentPath == "" {
		fmt.Fprintln(os.Stderr, "usage: client -agent /path/to/agent [-prompt text] [-- agent arguments]")
		os.Exit(2)
	}
	if err := run(*agentPath, flag.Args(), *prompt); err != nil {
		log.Fatal(err)
	}
}

var _ acp.Client = client{}
