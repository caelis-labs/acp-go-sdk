package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type minimalAgent struct{}

func (minimalAgent) Initialize(context.Context, InitializeRequest) (InitializeResponse, error) {
	return InitializeResponse{}, nil
}

func (minimalAgent) Cancel(context.Context, CancelNotification) error {
	return nil
}

func (minimalAgent) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{}, nil
}

func (minimalAgent) Prompt(context.Context, PromptRequest) (PromptResponse, error) {
	return PromptResponse{}, nil
}

func TestOmittedOptionalAgentCapabilityIsUnsupported(t *testing.T) {
	connectionSide, peerSide := net.Pipe()
	agent, err := NewAgentSideConnectionWithOptions(minimalAgent{}, connectionSide, connectionSide, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	defer func() { _ = peerSide.Close() }()
	if err := peerSide.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatal(err)
	}

	request := []byte(`{"jsonrpc":"2.0","id":"list","method":"session/list","params":{}}` + "\n")
	if _, err := peerSide.Write(request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(peerSide).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response anyMessage
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error == nil || response.Error.Code != -32601 {
		t.Fatalf("response error = %#v, want method not found", response.Error)
	}
}

func TestAgentDispatchRejectsMissingRequiredProperties(t *testing.T) {
	agent := &AgentSideConnection{
		agent:          minimalAgent{},
		sessionCancels: make(map[string]*sessionPromptCancel),
	}
	for _, params := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"protocolVersion":null}`),
	} {
		if _, requestErr := agent.handle(context.Background(), AgentMethodInitialize, params); requestErr == nil || requestErr.Code != -32602 {
			t.Fatalf("initialize params %s error = %#v, want invalid params", params, requestErr)
		}
	}
}

var _ Agent = minimalAgent{}

type concurrentPromptAgent struct {
	calls    atomic.Int32
	started  chan int
	canceled chan int
}

func (a *concurrentPromptAgent) Initialize(context.Context, InitializeRequest) (InitializeResponse, error) {
	return InitializeResponse{}, nil
}

func (a *concurrentPromptAgent) Cancel(context.Context, CancelNotification) error { return nil }

func (a *concurrentPromptAgent) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{}, nil
}

func (a *concurrentPromptAgent) Prompt(ctx context.Context, _ PromptRequest) (PromptResponse, error) {
	call := int(a.calls.Add(1))
	a.started <- call
	<-ctx.Done()
	a.canceled <- call
	return PromptResponse{}, ctx.Err()
}

func TestConcurrentPromptCleanupKeepsNewestCancel(t *testing.T) {
	impl := &concurrentPromptAgent{
		started:  make(chan int, 2),
		canceled: make(chan int, 2),
	}
	agent := &AgentSideConnection{
		agent:          impl,
		sessionCancels: make(map[string]*sessionPromptCancel),
	}
	params := json.RawMessage(`{"sessionId":"same","prompt":[]}`)
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = agent.handle(context.Background(), AgentMethodSessionPrompt, params)
	}()
	if got := <-impl.started; got != 1 {
		t.Fatalf("first started call = %d", got)
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = agent.handle(context.Background(), AgentMethodSessionPrompt, params)
	}()
	if got := <-impl.started; got != 2 {
		t.Fatalf("second started call = %d", got)
	}
	if got := <-impl.canceled; got != 1 {
		t.Fatalf("first canceled call = %d", got)
	}
	select {
	case <-firstDone:
	case <-time.After(testTimeout):
		t.Fatal("first prompt did not return after replacement")
	}

	cancelParams := json.RawMessage(`{"sessionId":"same"}`)
	if _, requestErr := agent.handle(context.Background(), AgentMethodSessionCancel, cancelParams); requestErr != nil {
		t.Fatal(requestErr)
	}
	if got := <-impl.canceled; got != 2 {
		t.Fatalf("second canceled call = %d", got)
	}
	select {
	case <-secondDone:
	case <-time.After(testTimeout):
		t.Fatal("newest prompt was not canceled")
	}
}

var _ Agent = (*concurrentPromptAgent)(nil)
