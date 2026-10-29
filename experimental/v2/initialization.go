package v2

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// initialization serializes handshake attempts and pins repeats to the selected
// profile. Notifications do not end the window for a repeated initialize.
type initialization struct {
	mu           sync.Mutex
	selected     ProtocolVersion
	initialized  bool
	initializing bool
	otherRequest bool
}

func (s *initialization) begin(selected ProtocolVersion) *acp.RequestError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initializing || (s.initialized && s.selected == 0) || (s.selected != 0 && (s.selected != 2 || selected != s.selected || s.otherRequest)) {
		return acp.NewInvalidRequest(map[string]any{"error": "initialize is not allowed in the current connection state"})
	}
	s.initialized = true
	s.initializing = true
	return nil
}

func (s *initialization) finish(selected ProtocolVersion, err *acp.RequestError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initializing = false
	if err == nil {
		s.selected = selected
	}
}

// route makes request admission atomic with ending the repeat window.
func (s *initialization) route(ctx context.Context) (ProtocolVersion, bool) {
	info, ok := acp.InboundInfoFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if (!ok || info.Kind == acp.InboundRequest) && s.initialized {
		s.otherRequest = true
	}
	return s.selected, s.initializing
}

func (s *initialization) checkRepeatVersion(requested ProtocolVersion) *acp.RequestError {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.selected == 2 && requested < 2 {
		return acp.NewInvalidRequest(map[string]any{"error": "a repeated initialize must use the selected protocol version"})
	}
	return nil
}

// NewUnsupportedProtocolVersion reports the v2 negotiation error without
// adding experimental error semantics to the stable root package.
func NewUnsupportedProtocolVersion(requested ProtocolVersion, supported []ProtocolVersion) *acp.RequestError {
	if supported == nil {
		supported = []ProtocolVersion{}
	}
	return &acp.RequestError{Code: -32022, Message: "Unsupported protocol version", Data: map[string]any{"requested": requested, "supported": supported}}
}

func validateRequestID(ctx context.Context) *acp.RequestError {
	info, ok := acp.InboundInfoFromContext(ctx)
	if !ok || info.Kind != acp.InboundRequest {
		return nil
	}
	var id RequestId
	if json.Unmarshal(info.RequestID, &id) != nil {
		return acp.NewInvalidRequest(map[string]any{"error": "ACP v2 requires a string or integer request ID"})
	}
	return nil
}

// A request written for a newer major may contain fields that this profile
// cannot parse. Preserve raw valid fields and unknown extensions; discard only
// known fields that cannot be decoded using the selected profile.
func normalizeInitialize(params json.RawMessage, requested, selected ProtocolVersion) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(params, &fields); err != nil {
		return nil, err
	}
	fields["protocolVersion"] = json.RawMessage("2")
	if requested > selected {
		var info Implementation
		if raw := fields["info"]; len(raw) == 0 || json.Unmarshal(raw, &info) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			fields["info"] = json.RawMessage(`{"name":"unknown","version":"unknown"}`)
		}
		var caps ClientCapabilities
		if raw := fields["capabilities"]; len(raw) > 0 && json.Unmarshal(raw, &caps) != nil {
			delete(fields, "capabilities")
		}
		var meta map[string]json.RawMessage
		if raw := fields["_meta"]; len(raw) > 0 && json.Unmarshal(raw, &meta) != nil {
			delete(fields, "_meta")
		}
	}
	return json.Marshal(fields)
}

func (a *AgentSideConnection) initialize(ctx context.Context, params json.RawMessage) (result any, reqErr *acp.RequestError) {
	// Version is inspected before version-specific fields, including required info.
	requested, err := ProtocolVersionFromInitialize(params)
	if err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	if reqErr := a.checkRepeatVersion(requested); reqErr != nil {
		return nil, reqErr
	}
	selected, err := SelectProtocolVersion(requested)
	if err != nil {
		return nil, NewUnsupportedProtocolVersion(requested, []ProtocolVersion{2})
	}
	if reqErr = a.begin(selected); reqErr != nil {
		return nil, reqErr
	}
	defer func() { a.finish(selected, reqErr) }()
	params, err = normalizeInitialize(params, requested, selected)
	if err != nil {
		return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	req, reqErr := decodeParams[InitializeRequest](params)
	if reqErr != nil {
		return nil, reqErr
	}
	resp, err := a.agent.Initialize(ctx, req)
	if err != nil {
		return nil, toRequestError(err)
	}
	if resp.ProtocolVersion != selected {
		return nil, acp.NewInvalidParams(map[string]any{"error": "agent returned a different protocol version"})
	}
	return resp, nil
}
