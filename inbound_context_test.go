package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestInboundInfoDistinguishesRequestsAndNotifications(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	seen := make(chan InboundInfo, 3)
	connection, err := NewConnectionWithOptions(
		func(ctx context.Context, _ string, _ json.RawMessage) (any, *RequestError) {
			info, ok := InboundInfoFromContext(ctx)
			if !ok {
				return nil, NewInternalError("missing inbound info")
			}
			if len(info.RequestID) > 0 {
				original := append(json.RawMessage(nil), info.RequestID...)
				info.RequestID[0] ^= 0xff
				again, ok := InboundInfoFromContext(ctx)
				if !ok || string(again.RequestID) != string(original) {
					return nil, NewInternalError("inbound request ID was not copied")
				}
				info.RequestID = original
			}
			seen <- info
			return nil, nil
		},
		connectionSide,
		connectionSide,
		testOptions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(peerSide)
	for _, rawID := range []string{`"request-id"`, "null"} {
		if _, err := fmt.Fprintf(peerSide, `{"jsonrpc":"2.0","id":%s,"method":"inspect","params":{}}`+"\n", rawID); err != nil {
			t.Fatal(err)
		}
		if _, err := reader.ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
		select {
		case info := <-seen:
			if info.Kind != InboundRequest || string(info.RequestID) != rawID {
				t.Fatalf("request info = %#v, want id %s", info, rawID)
			}
		case <-time.After(testTimeout):
			t.Fatal("request handler did not run")
		}
	}

	if _, err := io.WriteString(peerSide, `{"jsonrpc":"2.0","method":"inspect","params":{}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case info := <-seen:
		if info.Kind != InboundNotification || info.RequestID != nil {
			t.Fatalf("notification info = %#v", info)
		}
	case <-time.After(testTimeout):
		t.Fatal("notification handler did not run")
	}
}

type connectionAwareAgent struct {
	minimalAgent
	seen chan observedAgentConnection
}

type observedAgentConnection struct {
	method     string
	connection *AgentSideConnection
	info       InboundInfo
}

func (a *connectionAwareAgent) HandleExtensionMethod(ctx context.Context, method string, _ json.RawMessage) (any, error) {
	connection, ok := AgentSideConnectionFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing agent-side connection")
	}
	info, ok := InboundInfoFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing inbound info")
	}
	a.seen <- observedAgentConnection{method: method, connection: connection, info: info}
	return map[string]bool{"ok": true}, nil
}

func TestAgentHandlerContextUsesCurrentConnection(t *testing.T) {
	t.Parallel()
	implementation := &connectionAwareAgent{seen: make(chan observedAgentConnection, 2)}

	leftConnectionSide, leftPeerSide := net.Pipe()
	left, err := NewAgentSideConnectionWithOptions(implementation, leftConnectionSide, leftConnectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	defer func() { _ = leftPeerSide.Close() }()

	rightConnectionSide, rightPeerSide := net.Pipe()
	right, err := NewAgentSideConnectionWithOptions(implementation, rightConnectionSide, rightConnectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = right.Close() }()
	defer func() { _ = rightPeerSide.Close() }()

	for index, peer := range []net.Conn{leftPeerSide, rightPeerSide} {
		if err := peer.SetDeadline(time.Now().Add(testTimeout)); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(peer, `{"jsonrpc":"2.0","id":%d,"method":"_inspect","params":{}}`+"\n", index+1); err != nil {
			t.Fatal(err)
		}
		if _, err := bufio.NewReader(peer).ReadBytes('\n'); err != nil {
			t.Fatal(err)
		}
	}

	wantConnections := []*AgentSideConnection{left, right}
	for index, want := range wantConnections {
		select {
		case got := <-implementation.seen:
			if got.connection != want {
				t.Fatalf("handler %d connection = %p, want %p", index, got.connection, want)
			}
			if got.info.Kind != InboundRequest || string(got.info.RequestID) != fmt.Sprint(index+1) {
				t.Fatalf("handler %d info = %#v", index, got.info)
			}
		case <-time.After(testTimeout):
			t.Fatalf("handler %d did not run", index)
		}
	}
}

type connectionAwareClient struct {
	seen chan observedClientConnection
}

type observedClientConnection struct {
	connection *ClientSideConnection
	info       InboundInfo
}

func (c *connectionAwareClient) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}

func (c *connectionAwareClient) SessionUpdate(context.Context, SessionNotification) error {
	return nil
}

func (c *connectionAwareClient) HandleExtensionMethod(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
	connection, ok := ClientSideConnectionFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing client-side connection")
	}
	info, ok := InboundInfoFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing inbound info")
	}
	c.seen <- observedClientConnection{connection: connection, info: info}
	return nil, nil
}

func TestClientNotificationContextUsesCurrentConnection(t *testing.T) {
	t.Parallel()
	implementation := &connectionAwareClient{seen: make(chan observedClientConnection, 1)}
	connectionSide, peerSide := net.Pipe()
	connection, err := NewClientSideConnectionWithOptions(implementation, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()

	if _, err := io.WriteString(peerSide, `{"jsonrpc":"2.0","method":"_inspect","params":{}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-implementation.seen:
		if got.connection != connection {
			t.Fatalf("handler connection = %p, want %p", got.connection, connection)
		}
		if got.info.Kind != InboundNotification || got.info.RequestID != nil {
			t.Fatalf("handler info = %#v", got.info)
		}
	case <-time.After(testTimeout):
		t.Fatal("notification handler did not run")
	}
}

var _ Client = (*connectionAwareClient)(nil)
