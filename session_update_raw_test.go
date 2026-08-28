package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestSessionUpdateRawPreservesUnknownPayloads(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewAgentSideConnectionWithOptions(minimalAgent{}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	payloads := []json.RawMessage{
		json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"vendor_update","future":{"x":1}}}`),
		json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"future_content","payload":{"keep":true}},"extra":[1,2,3]}}`),
	}
	reader := bufio.NewReader(peerSide)
	for _, payload := range payloads {
		sendDone := make(chan error, 1)
		go func() {
			sendDone <- connection.SessionUpdateRaw(context.Background(), payload)
		}()
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		if err := <-sendDone; err != nil {
			t.Fatal(err)
		}
		var message anyMessage
		if err := json.Unmarshal(line, &message); err != nil {
			t.Fatal(err)
		}
		if message.ID != nil || message.Method != ClientMethodSessionUpdate {
			t.Fatalf("wire message = %#v, want session/update notification", message)
		}
		assertJSONSemanticEqual(t, message.Params, payload)
	}
}

func TestSessionUpdateRawValidatesOnlyOuterEnvelope(t *testing.T) {
	t.Parallel()
	connection := &AgentSideConnection{}
	for name, params := range map[string]json.RawMessage{
		"invalid JSON":     json.RawMessage(`{`),
		"array":            json.RawMessage(`[]`),
		"missing session":  json.RawMessage(`{"update":{}}`),
		"null session":     json.RawMessage(`{"sessionId":null,"update":{}}`),
		"numeric session":  json.RawMessage(`{"sessionId":1,"update":{}}`),
		"missing update":   json.RawMessage(`{"sessionId":"s1"}`),
		"null update":      json.RawMessage(`{"sessionId":"s1","update":null}`),
		"nonobject update": json.RawMessage(`{"sessionId":"s1","update":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			var requestErr *RequestError
			if err := connection.SessionUpdateRaw(context.Background(), params); !errors.As(err, &requestErr) || requestErr.Code != -32602 {
				t.Fatalf("error = %#v, want invalid params", err)
			}
		})
	}
}

type failingNotificationWriter struct{}

func (failingNotificationWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSessionUpdateRawUsesStructuredTransportErrors(t *testing.T) {
	t.Parallel()
	reader, writer := io.Pipe()
	connection, err := NewAgentSideConnectionWithOptions(minimalAgent{}, failingNotificationWriter{}, reader, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	defer func() { _ = connection.Close() }()

	err = connection.SessionUpdateRaw(context.Background(), json.RawMessage(`{"sessionId":"s1","update":{"sessionUpdate":"future"}}`))
	if !errors.Is(err, ErrTransportFailure) {
		t.Fatalf("error = %v, want ErrTransportFailure", err)
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) || transportErr.Op != TransportOperationWrite {
		t.Fatalf("error = %#v, want write TransportError", err)
	}
}

func assertJSONSemanticEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode got JSON: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode want JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", got, want)
	}
}
