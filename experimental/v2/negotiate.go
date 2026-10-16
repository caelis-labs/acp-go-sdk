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

// SelectProtocolVersion selects v2 for peers supporting v2 or a newer version.
func SelectProtocolVersion(requested ProtocolVersion) (ProtocolVersion, error) {
	if requested >= ProtocolVersionNumber {
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
	selected := requested
	switch {
	case r.V1 != nil && r.V2 == nil:
		handler = r.V1
		selected = 1
	case requested == 1:
		handler = r.V1
	case requested >= ProtocolVersionNumber:
		handler = r.V2
		selected = ProtocolVersionNumber
	default:
		return nil, acp.NewInvalidParams(map[string]any{"error": fmt.Sprintf("unsupported protocol version %d", requested)})
	}
	if handler == nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": fmt.Sprintf("protocol version %d is not configured", requested)})
	}
	if selected != requested {
		// Rewrite only the version. A typed decode/re-encode would discard
		// future fields and capability payloads needed by a downstream handler.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(params, &fields); err != nil {
			return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
		if selected == 1 && requested >= ProtocolVersionNumber {
			fields, err = v1InitializeFields(params, fields)
			if err != nil {
				return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
			}
		}
		fields["protocolVersion"] = json.RawMessage(fmt.Sprint(selected))
		params, err = json.Marshal(fields)
		if err != nil {
			return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
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
		r.selected = selected
	}
	r.mu.Unlock()
	return result, reqErr
}

// Only shared capabilities survive a fallback to the stable v1 surface.
// Unknown v2 capabilities must never enable client filesystem or terminal I/O.
func v1InitializeFields(params json.RawMessage, fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	var request InitializeRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	capabilities := make(map[string]json.RawMessage)
	var source map[string]json.RawMessage
	if raw := fields["capabilities"]; raw != nil {
		if err := json.Unmarshal(raw, &source); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"elicitation", "_meta"} {
		if raw := source[key]; raw != nil {
			capabilities[key] = raw
		}
	}
	if request.Capabilities.Auth != nil {
		terminal, err := json.Marshal(request.Capabilities.Auth.Terminal != nil)
		if err != nil {
			return nil, err
		}
		authFields := map[string]json.RawMessage{"terminal": terminal}
		if request.Capabilities.Auth.Meta != nil {
			meta, err := json.Marshal(request.Capabilities.Auth.Meta)
			if err != nil {
				return nil, err
			}
			authFields["_meta"] = meta
		}
		auth, err := json.Marshal(authFields)
		if err != nil {
			return nil, err
		}
		capabilities["auth"] = auth
	}
	rawCapabilities, err := json.Marshal(capabilities)
	if err != nil {
		return nil, err
	}
	result := map[string]json.RawMessage{"clientInfo": fields["info"], "clientCapabilities": rawCapabilities}
	if meta := fields["_meta"]; meta != nil {
		result["_meta"] = meta
	}
	return result, nil
}
