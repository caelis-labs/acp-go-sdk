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
	var req struct {
		ProtocolVersion *ProtocolVersion `json:"protocolVersion"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return 0, err
	}
	if req.ProtocolVersion == nil {
		return 0, fmt.Errorf("protocolVersion is required")
	}
	return *req.ProtocolVersion, nil
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

	mu          sync.Mutex
	selected    ProtocolVersion
	initialized bool
}

// Handle implements acp.MethodHandler.
func (r *ProtocolRouter) Handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	if method != AgentMethodInitialize {
		r.mu.Lock()
		selected := r.selected
		r.mu.Unlock()
		switch selected {
		case 1:
			return r.V1(ctx, method, params)
		case ProtocolVersionNumber:
			return r.V2(ctx, method, params)
		default:
			return nil, acp.NewInvalidRequest(map[string]any{"error": "initialize must succeed before other methods"})
		}
	}
	if info, ok := acp.InboundInfoFromContext(ctx); ok && info.Kind != acp.InboundRequest {
		return nil, acp.NewMethodNotFound(method)
	}
	requested, err := ProtocolVersionFromInitialize(params)
	if err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	var handler acp.MethodHandler
	switch requested {
	case 1:
		handler = r.V1
	case ProtocolVersionNumber:
		handler = r.V2
	default:
		return nil, acp.NewInvalidParams(map[string]any{"error": fmt.Sprintf("unsupported protocol version %d", requested)})
	}
	if handler == nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": fmt.Sprintf("protocol version %d is not configured", requested)})
	}
	r.mu.Lock()
	if r.selected != 0 || r.initialized {
		r.mu.Unlock()
		return nil, acp.NewInvalidRequest(map[string]any{"error": "initialize has already been received"})
	}
	r.initialized = true
	r.mu.Unlock()
	result, reqErr := handler(ctx, method, params)
	r.mu.Lock()
	if reqErr == nil {
		r.selected = requested
	}
	r.mu.Unlock()
	return result, reqErr
}
