package acp

import (
	"context"
	"encoding/json"
)

// InboundKind identifies whether the current handler invocation came from a
// JSON-RPC request or notification.
type InboundKind uint8

const (
	// InboundRequest requires the connection to send a JSON-RPC response.
	InboundRequest InboundKind = iota + 1
	// InboundNotification does not have a JSON-RPC response.
	InboundNotification
)

// InboundInfo describes the JSON-RPC message currently being handled.
// RequestID is present only for requests and preserves the original JSON
// representation, including string, number, and explicit null IDs.
type InboundInfo struct {
	Kind      InboundKind
	RequestID json.RawMessage
}

type inboundInfoContextKey struct{}
type inboundParamsContextKey struct{}
type agentSideConnectionContextKey struct{}
type clientSideConnectionContextKey struct{}

// InboundInfoFromContext returns metadata for the current inbound handler
// invocation. The returned request ID is a copy and may be retained or
// modified by the caller.
func InboundInfoFromContext(ctx context.Context) (InboundInfo, bool) {
	info, ok := ctx.Value(inboundInfoContextKey{}).(InboundInfo)
	if !ok {
		return InboundInfo{}, false
	}
	info.RequestID = append(json.RawMessage(nil), info.RequestID...)
	return info, true
}

// InboundParamsFromContext returns the lossless JSON-RPC params for the
// current inbound handler invocation. The returned bytes are a defensive copy
// and may be retained or modified by the caller. A nil RawMessage with a true
// boolean means the inbound message omitted params.
func InboundParamsFromContext(ctx context.Context) (json.RawMessage, bool) {
	params, ok := ctx.Value(inboundParamsContextKey{}).(json.RawMessage)
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), params...), true
}

// AgentSideConnectionFromContext returns the current agent-side connection
// while an Agent or agent-side extension handler is running.
func AgentSideConnectionFromContext(ctx context.Context) (*AgentSideConnection, bool) {
	connection, ok := ctx.Value(agentSideConnectionContextKey{}).(*AgentSideConnection)
	return connection, ok && connection != nil
}

// ClientSideConnectionFromContext returns the current client-side connection
// while a Client or client-side extension handler is running.
func ClientSideConnectionFromContext(ctx context.Context) (*ClientSideConnection, bool) {
	connection, ok := ctx.Value(clientSideConnectionContextKey{}).(*ClientSideConnection)
	return connection, ok && connection != nil
}

func withInboundInfo(ctx context.Context, req *anyMessage) context.Context {
	info := InboundInfo{Kind: InboundNotification}
	if req.ID != nil {
		info.Kind = InboundRequest
		info.RequestID = append(json.RawMessage(nil), (*req.ID)...)
	}
	ctx = context.WithValue(ctx, inboundInfoContextKey{}, info)
	return context.WithValue(ctx, inboundParamsContextKey{}, append(json.RawMessage(nil), req.Params...))
}

func requireInboundKind(ctx context.Context, want InboundKind, method string) *RequestError {
	info, ok := InboundInfoFromContext(ctx)
	if !ok {
		return NewInternalError(map[string]any{"error": "ACP inbound message metadata is unavailable"})
	}
	if info.Kind != want {
		return NewMethodNotFound(method)
	}
	return nil
}
