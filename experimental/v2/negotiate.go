package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// ProtocolVersionFromInitialize reads protocolVersion from initialize params.
func ProtocolVersionFromInitialize(params json.RawMessage) (ProtocolVersion, error) {
	var req InitializeRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return 0, err
	}
	return req.ProtocolVersion, nil
}

// SelectProtocolVersion returns the v2 version when the peer asked for it.
func SelectProtocolVersion(requested ProtocolVersion) (ProtocolVersion, error) {
	if requested == ProtocolVersionNumber {
		return ProtocolVersionNumber, nil
	}
	return 0, fmt.Errorf("unsupported protocol version %d, experimental v2 requires %d", requested, ProtocolVersionNumber)
}

// ProtocolRouter pins v1 or v2 on the first initialize request and then
// forwards later methods to that implementation. It is experimental and does
// not mix v1 and v2 types in the stable root package.
type ProtocolRouter struct {
	V1 acp.MethodHandler
	V2 acp.MethodHandler

	mu       sync.Mutex
	selected ProtocolVersion
}

// Handle implements acp.MethodHandler.
func (r *ProtocolRouter) Handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	if method == AgentMethodInitialize {
		requested, err := ProtocolVersionFromInitialize(params)
		if err != nil {
			return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
		r.mu.Lock()
		r.selected = requested
		r.mu.Unlock()
	}
	r.mu.Lock()
	selected := r.selected
	r.mu.Unlock()
	switch selected {
	case ProtocolVersionNumber:
		if r.V2 == nil {
			return nil, acp.NewInternalError(map[string]any{"error": "v2 handler is not configured"})
		}
		return r.V2(ctx, method, params)
	case 1:
		if r.V1 == nil {
			return nil, acp.NewInternalError(map[string]any{"error": "v1 handler is not configured"})
		}
		return r.V1(ctx, method, params)
	default:
		if method != AgentMethodInitialize {
			return nil, acp.NewInvalidRequest(map[string]any{"error": "initialize is required before other methods"})
		}
		return nil, acp.NewInvalidParams(map[string]any{"error": fmt.Sprintf("unsupported protocol version %d", selected)})
	}
}
