package acp

import (
	"context"
	"io"
	"log/slog"
	"sync"
)

// AgentSideConnection represents the agent's view of a connection to a client.
type AgentSideConnection struct {
	conn  *Connection
	agent Agent

	mu             sync.Mutex
	sessionCancels map[string]*sessionPromptCancel
}

type sessionPromptCancel struct {
	cancel context.CancelFunc
}

// NewAgentSideConnection creates a new agent-side connection bound to the
// provided Agent implementation.
func NewAgentSideConnection(agent Agent, peerInput io.Writer, peerOutput io.Reader) *AgentSideConnection {
	asc, err := NewAgentSideConnectionWithOptions(agent, peerInput, peerOutput, ConnectionOptions{})
	if err != nil {
		panic(err)
	}
	return asc
}

// NewAgentSideConnectionWithOptions creates a bounded agent-side connection.
// Agent callbacks cannot begin until the returned peer object is fully
// initialized and safe for reverse calls. Stream ownership transfers only on
// successful construction.
func NewAgentSideConnectionWithOptions(agent Agent, peerInput io.Writer, peerOutput io.Reader, opts ConnectionOptions) (*AgentSideConnection, error) {
	asc := &AgentSideConnection{
		agent:          agent,
		sessionCancels: make(map[string]*sessionPromptCancel),
	}
	conn, err := constructConnection(asc.handleWithExtensions, peerInput, peerOutput, opts)
	if err != nil {
		return nil, err
	}
	asc.conn = conn
	conn.start()
	return asc, nil
}

// Done exposes a channel that closes when the peer disconnects.
func (c *AgentSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// Close idempotently closes the connection.
func (c *AgentSideConnection) Close() error { return c.conn.Close() }

// Wait waits for all connection-owned goroutines to terminate.
func (c *AgentSideConnection) Wait(ctx context.Context) error { return c.conn.Wait(ctx) }

// Err reports the connection shutdown cause.
func (c *AgentSideConnection) Err() error { return c.conn.Err() }

// SetLogger directs connection diagnostics to the provided logger.
func (c *AgentSideConnection) SetLogger(l *slog.Logger) { c.conn.SetLogger(l) }
