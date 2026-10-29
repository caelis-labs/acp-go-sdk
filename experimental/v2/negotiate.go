package v2

import (
	"context"
	"encoding/json"
	"fmt"

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

	initialization
}

// Handle implements acp.MethodHandler.
func (r *ProtocolRouter) Handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	if method != AgentMethodInitialize {
		selected, pending := r.route(ctx)
		if pending {
			return nil, acp.NewInvalidRequest(nil)
		}
		if selected == 2 {
			if err := validateRequestID(ctx); err != nil {
				return nil, err
			}
		}
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
	if err := r.checkRepeatVersion(requested); err != nil {
		return nil, err
	}
	var handler acp.MethodHandler
	selected := ProtocolVersion(0)
	switch {
	case r.V1 != nil && r.V2 == nil:
		handler, selected = r.V1, 1 // Legacy v1-only fallback.
	case requested >= 2 && r.V2 != nil:
		handler, selected = r.V2, 2
	case requested >= 1 && r.V1 != nil:
		handler, selected = r.V1, 1
	}
	if handler == nil {
		supported := []ProtocolVersion{}
		if r.V1 != nil {
			supported = append(supported, 1)
		}
		if r.V2 != nil {
			supported = append(supported, 2)
		}
		return nil, NewUnsupportedProtocolVersion(requested, supported)
	}
	if selected == 2 {
		if err := validateRequestID(ctx); err != nil {
			return nil, err
		}
	}
	if selected == 2 || (selected == 1 && requested >= 2) {
		params, err = normalizeInitialize(params, requested, selected)
		if err != nil {
			return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
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
	if err := r.begin(selected); err != nil {
		return nil, err
	}
	result, reqErr := handler(ctx, method, params)
	r.finish(selected, reqErr)
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
