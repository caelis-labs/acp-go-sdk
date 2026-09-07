package v2

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestRouterPinsVersionAndAcceptsV1InitializeShape(t *testing.T) {
	var v1Calls, v2Calls int
	r := ProtocolRouter{
		V1: func(context.Context, string, json.RawMessage) (any, *acp.RequestError) { v1Calls++; return nil, nil },
		V2: func(context.Context, string, json.RawMessage) (any, *acp.RequestError) { v2Calls++; return nil, nil },
	}
	ctx := context.Background()
	if _, err := r.Handle(ctx, "initialize", json.RawMessage(`{"protocolVersion":1,"clientInfo":{"name":"v1","version":"0"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"protocolVersion":2}`, `{"protocolVersion":1}`, `{"protocolVersion":999}`} {
		if _, err := r.Handle(ctx, "initialize", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted repeated initialize %s", raw)
		}
	}
	if _, err := r.Handle(ctx, "session/new", nil); err != nil {
		t.Fatal(err)
	}
	if v1Calls != 2 || v2Calls != 0 {
		t.Fatalf("calls v1=%d v2=%d", v1Calls, v2Calls)
	}
}

func TestRouterDoesNotRouteBeforeInitializationSucceeds(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := ProtocolRouter{V2: func(context.Context, string, json.RawMessage) (any, *acp.RequestError) {
		calls.Add(1)
		close(started)
		<-release
		return nil, acp.NewAuthRequired(nil)
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = r.Handle(context.Background(), "initialize", json.RawMessage(`{"protocolVersion":2}`))
	}()
	<-started
	if _, err := r.Handle(context.Background(), "initialize", json.RawMessage(`{"protocolVersion":2}`)); err == nil {
		t.Fatal("concurrent initialize accepted")
	}
	if _, err := r.Handle(context.Background(), "session/new", nil); err == nil {
		t.Fatal("request routed while initialize pending")
	}
	close(release)
	<-done
	if _, err := r.Handle(context.Background(), "session/new", nil); err == nil {
		t.Fatal("request routed after failed initialize")
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	r.V2 = func(context.Context, string, json.RawMessage) (any, *acp.RequestError) { return nil, nil }
	if _, err := r.Handle(context.Background(), "initialize", json.RawMessage(`{"protocolVersion":2}`)); err == nil {
		t.Fatal("accepted initialize after failed handshake")
	}
}

func TestRouterNotificationCannotPinVersion(t *testing.T) {
	r := ProtocolRouter{V2: func(context.Context, string, json.RawMessage) (any, *acp.RequestError) {
		t.Fatal("notification reached handler")
		return nil, nil
	}}
	if _, err := r.Handle(inboundContext(t, true), "initialize", json.RawMessage(`{"protocolVersion":2}`)); err == nil {
		t.Fatal("notification accepted")
	}
	if r.selected != 0 {
		t.Fatal("notification selected version")
	}
}
