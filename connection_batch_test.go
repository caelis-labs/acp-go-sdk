package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestConnectionAcceptsJSONRPCBatchAndRepliesAsArray(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return map[string]any{"method": method}, nil
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

	batch := `[{"jsonrpc":"2.0","id":1,"method":"one"},{"jsonrpc":"2.0","id":2,"method":"two"}]` + "\n"
	if _, err := io.WriteString(peerSide, batch); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var replies []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(line, &replies); err != nil {
		t.Fatalf("response %s is not a batch array: %v", line, err)
	}
	if len(replies) != 2 || string(replies[0].ID) != "1" || string(replies[1].ID) != "2" {
		t.Fatalf("replies = %#v", replies)
	}
}

func TestConnectionNotificationOnlyBatchProducesNoResponse(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	seen := make(chan string, 1)
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			seen <- method
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

	if _, err := io.WriteString(peerSide, `[{"jsonrpc":"2.0","method":"note"}]`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case method := <-seen:
		if method != "note" {
			t.Fatalf("method = %q", method)
		}
	case <-time.After(testTimeout):
		t.Fatal("notification was not dispatched")
	}

	_ = peerSide.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 16)
	if n, err := peerSide.Read(buf); err == nil {
		t.Fatalf("unexpected response %q", buf[:n])
	}
}

func TestConnectionEmptyBatchReturnsInvalidRequest(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, "[]\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID    json.RawMessage `json:"id"`
		Error *RequestError   `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if string(response.ID) != "null" || response.Error == nil || response.Error.Code != -32600 {
		t.Fatalf("response = %#v", response)
	}
}

func TestConnectionMalformedFrameReturnsParseError(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(nil, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	if _, err := io.WriteString(peerSide, "{\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error *RequestError `json:"error"`
	}
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != -32700 {
		t.Fatalf("response = %#v", response)
	}
}

func TestConnectionBatchMixedInvalidEntryKeepsSiblings(t *testing.T) {
	t.Parallel()
	connectionSide, peerSide := net.Pipe()
	connection, err := NewConnectionWithOptions(
		func(_ context.Context, method string, _ json.RawMessage) (any, *RequestError) {
			return map[string]any{"method": method}, nil
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

	if _, err := io.WriteString(peerSide, `[{"jsonrpc":"2.0","id":1,"method":"ok"},"nope"]`+"\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var replies []struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *RequestError   `json:"error"`
	}
	if err := json.Unmarshal(line, &replies); err != nil {
		t.Fatalf("response %s: %v", line, err)
	}
	if len(replies) != 2 || string(replies[0].ID) != "1" || replies[0].Error != nil {
		t.Fatalf("replies = %#v", replies)
	}
	if string(replies[1].ID) != "null" || replies[1].Error == nil || replies[1].Error.Code != -32600 {
		t.Fatalf("invalid entry reply = %#v", replies[1])
	}
}

func TestSendTransportFramePreservesBatchBoundary(t *testing.T) {
	t.Parallel()
	senderSide, receiverSide := net.Pipe()
	sender, err := NewConnectionWithOptions(nil, senderSide, senderSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	defer func() { _ = receiverSide.Close() }()
	if err := receiverSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	raw := []byte(`[{"jsonrpc":"2.0","id":1,"method":"echo"},{"jsonrpc":"2.0","method":"note"}]`)
	frame := ParseTransportFrame(raw)
	if frame.Kind != FrameKindBatch {
		t.Fatalf("kind = %s", frame.Kind)
	}
	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(receiverSide).ReadBytes('\n')
		if err != nil {
			errCh <- err
			return
		}
		done <- line
	}()
	ctx, cancel := waitContext(t)
	defer cancel()
	if err := sender.SendTransportFrame(ctx, frame); err != nil {
		t.Fatal(err)
	}
	var line []byte
	select {
	case err := <-errCh:
		t.Fatal(err)
	case line = <-done:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if string(line) != string(raw)+"\n" {
		t.Fatalf("relayed %s, want original batch", line)
	}
	var decoded []json.RawMessage
	if err := json.Unmarshal(line, &decoded); err != nil || len(decoded) != 2 {
		t.Fatalf("relayed frame was flattened: %s", line)
	}
}
