package stdio

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestDuplicateFileHasIndependentLifetime(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdio-duplicate-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	duplicate, err := DuplicateFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Fd() == file.Fd() {
		t.Fatalf("duplicate descriptor = original descriptor %d", file.Fd())
	}
	if err := duplicate.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("original remains open"); err != nil {
		t.Fatalf("original file was closed with duplicate: %v", err)
	}
}

func TestDuplicateFileRejectsNil(t *testing.T) {
	if _, err := DuplicateFile(nil); err == nil {
		t.Fatal("DuplicateFile(nil) succeeded")
	}
}

func TestDuplicateFileRejectsClosedFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdio-closed-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := DuplicateFile(file); err == nil {
		t.Fatal("DuplicateFile(closed) succeeded")
	}
}

type noopAgent struct{}

func (noopAgent) Initialize(context.Context, acp.InitializeRequest) (acp.InitializeResponse, error) {
	return acp.InitializeResponse{}, nil
}

func (noopAgent) NewSession(context.Context, acp.NewSessionRequest) (acp.NewSessionResponse, error) {
	return acp.NewSessionResponse{}, nil
}

func (noopAgent) Prompt(context.Context, acp.PromptRequest) (acp.PromptResponse, error) {
	return acp.PromptResponse{}, nil
}

func (noopAgent) Cancel(context.Context, acp.CancelNotification) error { return nil }

func TestAgentConnectionDoesNotOwnProcessFiles(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "stdio-input-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.CreateTemp(t.TempDir(), "stdio-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()

	connection, err := newAgentConnection(noopAgent{}, input, output, acp.ConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := connection.Wait(waitCtx); err != nil && !errors.Is(err, acp.ErrConnectionClosed) && !errors.Is(err, acp.ErrPeerClosed) {
		t.Fatal(err)
	}
	if _, err := input.Stat(); err != nil {
		t.Fatalf("process input was closed: %v", err)
	}
	if _, err := output.WriteString("process output remains open"); err != nil {
		t.Fatalf("process output was closed: %v", err)
	}
}

var _ acp.Agent = noopAgent{}
