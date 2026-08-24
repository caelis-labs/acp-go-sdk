package stdio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("ACP_STDIO_BLOCK_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if os.Getenv("ACP_STDIO_HELPER") != "1" {
		return
	}
	_, _ = io.WriteString(os.Stderr, strings.Repeat("stderr must be drained\n", 2048))
	_, _ = io.WriteString(os.Stdout, "{\"jsonrpc\":\"2.0\",\"method\":\"ready\"}\n")
	os.Exit(0)
}

type noopClient struct{}

func (noopClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{}, nil
}

func (noopClient) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }

func TestClientProcessWaitTimeoutDoesNotCloseConnection(t *testing.T) {
	process, err := StartClient(context.Background(), noopClient{}, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_BLOCK_HELPER=1"),
	}, acp.ConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := process.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want deadline exceeded", err)
	}
	select {
	case <-process.Connection.Done():
		t.Fatal("Wait timeout closed the active protocol connection")
	default:
	}
}

var _ acp.Client = noopClient{}

func TestStartDrainsStderrAndWaitIsRepeatable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	process, err := Start(ctx, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_HELPER=1"),
		Stderr:     &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	protocol, err := io.ReadAll(process.Output())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(ctx); err != nil {
		t.Fatalf("second Wait = %v", err)
	}
	if got := string(protocol); got != "{\"jsonrpc\":\"2.0\",\"method\":\"ready\"}\n" {
		t.Fatalf("protocol stdout = %q", got)
	}
	if got := strings.Count(stderr.String(), "stderr must be drained"); got != 2048 {
		t.Fatalf("drained stderr lines = %d, want 2048", got)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}
