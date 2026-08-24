// Package stdio provides the stable ACP newline-delimited JSON transport.
package stdio

import (
	"context"
	"os"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// NewAgentConnection binds an Agent to the current process stdin/stdout.
// Protocol messages are written only to stdout; application logs belong on
// stderr.
func NewAgentConnection(agent acp.Agent, opts acp.ConnectionOptions) (*acp.AgentSideConnection, error) {
	return acp.NewAgentSideConnectionWithOptions(agent, os.Stdout, os.Stdin, opts)
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
