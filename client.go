package acp

import (
	"context"
	"io"
	"log/slog"
)

// ClientSideConnection provides the client's view of the connection and implements Agent calls.
type ClientSideConnection struct {
	conn   *Connection
	client Client
}

// NewClientSideConnection creates a new client-side connection bound to the
// provided Client implementation.
func NewClientSideConnection(client Client, peerInput io.Writer, peerOutput io.Reader) *ClientSideConnection {
	csc, err := NewClientSideConnectionWithOptions(client, peerInput, peerOutput, ConnectionOptions{})
	if err != nil {
		panic(err)
	}
	return csc
}

// NewClientSideConnectionWithOptions creates a bounded client-side connection.
func NewClientSideConnectionWithOptions(client Client, peerInput io.Writer, peerOutput io.Reader, opts ConnectionOptions) (*ClientSideConnection, error) {
	csc := &ClientSideConnection{client: client}
	conn, err := NewConnectionWithOptions(csc.handleWithExtensions, peerInput, peerOutput, opts)
	if err != nil {
		return nil, err
	}
	csc.conn = conn
	return csc, nil
}

// Done exposes a channel that closes when the peer disconnects.
func (c *ClientSideConnection) Done() <-chan struct{} { return c.conn.Done() }

// Close idempotently closes the connection.
func (c *ClientSideConnection) Close() error { return c.conn.Close() }

// Wait waits for all connection-owned goroutines to terminate.
func (c *ClientSideConnection) Wait(ctx context.Context) error { return c.conn.Wait(ctx) }

// Err reports the connection shutdown cause.
func (c *ClientSideConnection) Err() error { return c.conn.Err() }

// SetLogger directs connection diagnostics to the provided logger.
func (c *ClientSideConnection) SetLogger(l *slog.Logger) { c.conn.SetLogger(l) }
