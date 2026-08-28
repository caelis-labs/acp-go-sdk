// Package stdio provides the stable ACP newline-delimited JSON transport.
package stdio

import (
	"context"
	"fmt"
	"os"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// NewAgentConnection binds an Agent to the current process stdin/stdout.
// Protocol messages are written only to stdout; application logs belong on
// stderr.
func NewAgentConnection(agent acp.Agent, opts acp.ConnectionOptions) (*acp.AgentSideConnection, error) {
	return newAgentConnection(agent, os.Stdin, os.Stdout, opts)
}

func newAgentConnection(agent acp.Agent, processInput, processOutput *os.File, opts acp.ConnectionOptions) (*acp.AgentSideConnection, error) {
	input, err := DuplicateFile(processInput)
	if err != nil {
		return nil, fmt.Errorf("stdio: duplicate process input: %w", err)
	}
	output, err := DuplicateFile(processOutput)
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("stdio: duplicate process output: %w", err)
	}
	connection, err := acp.NewAgentSideConnectionWithOptions(agent, output, input, opts)
	if err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, err
	}
	return connection, nil
}

// ServeAgent runs an Agent over the current process stdin/stdout until the
// peer disconnects, the connection fails, or ctx is canceled.
func ServeAgent(ctx context.Context, agent acp.Agent, opts acp.ConnectionOptions) error {
	connection, err := NewAgentConnection(agent, opts)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- connection.Wait(context.Background())
	}()
	select {
	case <-ctx.Done():
		_ = connection.Close()
		return context.Cause(ctx)
	case err := <-waitDone:
		return err
	}
}
