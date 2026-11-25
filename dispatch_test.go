package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
		if _, requestErr := agent.handle(testInboundContext(InboundRequest, params), AgentMethodInitialize, params); requestErr == nil || requestErr.Code != -32602 {
			t.Fatalf("initialize params %s error = %#v, want invalid params", params, requestErr)
		}
	}
}

var _ Agent = minimalAgent{}

type configAgent struct {
	minimalAgent
	calls chan SetSessionConfigOptionRequest
}

func (a configAgent) SetSessionConfigOption(_ context.Context, request SetSessionConfigOptionRequest) (SetSessionConfigOptionResponse, error) {
	a.calls <- request
	return SetSessionConfigOptionResponse{ConfigOptions: []SessionConfigOption{}}, nil
}

func TestSetSessionConfigOptionWireVariants(t *testing.T) {
	agentStream, clientStream := net.Pipe()
	calls := make(chan SetSessionConfigOptionRequest, 4)
	agent := NewAgentSideConnection(configAgent{calls: calls}, agentStream, agentStream)
	t.Cleanup(func() { _ = agent.Close() })
	client := NewClientSideConnection(nil, clientStream, clientStream)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	for _, value := range []bool{true, false} {
		request := SetSessionConfigOptionRequest{Boolean: &SetSessionConfigOptionBoolean{
			SessionId: "s", ConfigId: "verbose", Value: value,
		}}
		if _, err := client.SetSessionConfigOption(ctx, request); err != nil {
			t.Fatal(err)
		}
		received := <-calls
		if received.Boolean == nil || received.Boolean.Value != value || received.ValueId != nil {
			t.Fatalf("unexpected boolean variant: %+v", received)
		}
	}
	request := SetSessionConfigOptionRequest{ValueId: &SetSessionConfigOptionValueId{
		SessionId: "s", ConfigId: "model", Value: "fast",
	}}
	if _, err := client.SetSessionConfigOption(ctx, request); err != nil {
		t.Fatal(err)
	}
	received := <-calls
	if received.ValueId == nil || received.ValueId.Value != "fast" || received.Boolean != nil {
		t.Fatalf("unexpected string-ID variant: %+v", received)
	}

	for _, params := range []json.RawMessage{
		json.RawMessage(`{"sessionId":"s","configId":"model","type":"boolean","value":"fast"}`),
		json.RawMessage(`{"sessionId":"s","configId":"verbose","type":"boolean","value":null}`),
		json.RawMessage(`{"sessionId":"s","configId":"verbose","value":false}`),
	} {
		_, err := SendRequest[SetSessionConfigOptionResponse](client.conn, ctx, AgentMethodSessionSetConfigOption, params)
		var remote *RequestError
		if !errors.As(err, &remote) || remote.Code != -32602 {
			t.Fatalf("%s: expected Invalid params, got %v", params, err)
		}
	}
	if len(calls) != 0 {
		t.Fatal("invalid requests reached the agent")
	}
}

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
		_, _ = agent.handle(testInboundContext(InboundRequest, params), AgentMethodSessionPrompt, params)
	}()
	if got := <-impl.started; got != 1 {
		t.Fatalf("first started call = %d", got)
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = agent.handle(testInboundContext(InboundRequest, params), AgentMethodSessionPrompt, params)
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
	if _, requestErr := agent.handle(testInboundContext(InboundNotification, cancelParams), AgentMethodSessionCancel, cancelParams); requestErr != nil {
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

func testInboundContext(kind InboundKind, params json.RawMessage) context.Context {
	message := &anyMessage{Params: append(json.RawMessage(nil), params...)}
	if kind == InboundRequest {
		id := json.RawMessage(`1`)
		message.ID = &id
	}
	return withInboundInfo(context.Background(), message)
}

type directionAgent struct {
	minimalAgent
	cancelCalls atomic.Int32
	promptCalls atomic.Int32
	closeCalls  atomic.Int32
}

func (a *directionAgent) Cancel(context.Context, CancelNotification) error {
	a.cancelCalls.Add(1)
	return nil
}

func (a *directionAgent) Prompt(context.Context, PromptRequest) (PromptResponse, error) {
	a.promptCalls.Add(1)
	return PromptResponse{}, nil
}

func (a *directionAgent) CloseSession(context.Context, CloseSessionRequest) (CloseSessionResponse, error) {
	a.closeCalls.Add(1)
	return CloseSessionResponse{}, nil
}

func TestAgentDispatchRejectsWrongDirectionBeforeSideEffects(t *testing.T) {
	implementation := &directionAgent{}
	cancelCalls := atomic.Int32{}
	agent := &AgentSideConnection{
		agent: implementation,
		sessionCancels: map[string]*sessionPromptCancel{
			"active": {cancel: func() { cancelCalls.Add(1) }},
		},
	}

	cancelParams := json.RawMessage(`{"sessionId":"active"}`)
	if _, requestErr := agent.handle(testInboundContext(InboundRequest, cancelParams), AgentMethodSessionCancel, cancelParams); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("cancel request error = %#v, want method not found", requestErr)
	}
	if got := implementation.cancelCalls.Load(); got != 0 {
		t.Fatalf("Cancel calls = %d, want 0", got)
	}
	if got := cancelCalls.Load(); got != 0 {
		t.Fatalf("prompt cancel calls = %d, want 0", got)
	}
	if _, ok := agent.sessionCancels["active"]; !ok {
		t.Fatal("wrong-direction cancel mutated session cancellation state")
	}

	malformed := json.RawMessage(`{`)
	if _, requestErr := agent.handle(testInboundContext(InboundRequest, malformed), AgentMethodSessionCancel, malformed); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("malformed cancel request error = %#v, want direction error before decode", requestErr)
	}

	promptParams := json.RawMessage(`{"sessionId":"active","prompt":[]}`)
	if _, requestErr := agent.handle(testInboundContext(InboundNotification, promptParams), AgentMethodSessionPrompt, promptParams); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("prompt notification error = %#v, want method not found", requestErr)
	}
	if got := implementation.promptCalls.Load(); got != 0 {
		t.Fatalf("Prompt calls = %d, want 0", got)
	}
	if len(agent.sessionCancels) != 1 {
		t.Fatalf("session cancellation entries = %d, want 1", len(agent.sessionCancels))
	}

	closeParams := json.RawMessage(`{"sessionId":"active"}`)
	if _, requestErr := agent.handle(testInboundContext(InboundNotification, closeParams), AgentMethodSessionClose, closeParams); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("close notification error = %#v, want method not found", requestErr)
	}
	if got := implementation.closeCalls.Load(); got != 0 {
		t.Fatalf("CloseSession calls = %d, want 0", got)
	}
}

type directionClient struct {
	permissionCalls atomic.Int32
	updateCalls     atomic.Int32
}

func (c *directionClient) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	c.permissionCalls.Add(1)
	return RequestPermissionResponse{}, nil
}

func (c *directionClient) SessionUpdate(context.Context, SessionNotification) error {
	c.updateCalls.Add(1)
	return nil
}

func TestClientDispatchRejectsWrongDirectionBeforeCallbacks(t *testing.T) {
	implementation := &directionClient{}
	client := &ClientSideConnection{client: implementation}

	updateParams := json.RawMessage(`{"sessionId":"active","update":{"sessionUpdate":"future"}}`)
	if _, requestErr := client.handle(testInboundContext(InboundRequest, updateParams), ClientMethodSessionUpdate, updateParams); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("update request error = %#v, want method not found", requestErr)
	}
	if got := implementation.updateCalls.Load(); got != 0 {
		t.Fatalf("SessionUpdate calls = %d, want 0", got)
	}

	malformed := json.RawMessage(`{`)
	if _, requestErr := client.handle(testInboundContext(InboundNotification, malformed), ClientMethodSessionRequestPermission, malformed); requestErr == nil || requestErr.Code != -32601 {
		t.Fatalf("permission notification error = %#v, want direction error before decode", requestErr)
	}
	if got := implementation.permissionCalls.Load(); got != 0 {
		t.Fatalf("RequestPermission calls = %d, want 0", got)
	}
}

var _ AgentSessionCloser = (*directionAgent)(nil)
var _ Client = (*directionClient)(nil)
