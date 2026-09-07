package acp

import (
	"bytes"
	"encoding/json"
	"errors"
)

// FrameKind classifies one JSON-RPC transport value.
type FrameKind uint8

const jsonRPCMethodCancelRequest = "$/cancel_request"

const (
	// FrameKindSingle is one JSON object.
	FrameKindSingle FrameKind = iota + 1
	// FrameKindBatch is one non-empty JSON array.
	FrameKindBatch
	// FrameKindMalformed is a raw value that is not a valid JSON-RPC object or
	// non-empty batch. Relays must still forward Raw.
	FrameKindMalformed
)

func (k FrameKind) String() string {
	switch k {
	case FrameKindSingle:
		return "single"
	case FrameKindBatch:
		return "batch"
	case FrameKindMalformed:
		return "malformed"
	default:
		return "unknown"
	}
}

// TransportFrame is one NDJSON transport value. A frame preserves the boundary
// between a single JSON-RPC message and a batch. Relays must forward the
// complete frame; flattening a batch into independent objects changes JSON-RPC
// semantics.
type TransportFrame struct {
	Raw  json.RawMessage
	Kind FrameKind

	single  anyMessage
	entries []batchEntry
	err     *RequestError
}

type batchEntry struct {
	raw json.RawMessage
	msg anyMessage
	ok  bool
	err *RequestError
}

// ParseTransportFrame classifies one JSON value. raw is copied; the caller may
// reuse the input buffer.
func ParseTransportFrame(raw []byte) TransportFrame {
	trimmed := bytes.TrimSpace(raw)
	copied := append(json.RawMessage(nil), trimmed...)
	if len(copied) == 0 || !json.Valid(copied) {
		return TransportFrame{Raw: copied, Kind: FrameKindMalformed, err: NewParseError(nil)}
	}
	switch copied[0] {
	case '{':
		var msg anyMessage
		if err := json.Unmarshal(copied, &msg); err != nil {
			return TransportFrame{Raw: copied, Kind: FrameKindMalformed, err: NewParseError(nil)}
		}
		return TransportFrame{Raw: copied, Kind: FrameKindSingle, single: msg}
	case '[':
		var elems []json.RawMessage
		if err := json.Unmarshal(copied, &elems); err != nil {
			return TransportFrame{Raw: copied, Kind: FrameKindMalformed, err: NewParseError(nil)}
		}
		if len(elems) == 0 {
			return TransportFrame{Raw: copied, Kind: FrameKindMalformed, err: NewInvalidRequest(nil)}
		}
		entries := make([]batchEntry, len(elems))
		for i, elem := range elems {
			entries[i] = parseBatchEntry(elem)
		}
		return TransportFrame{Raw: copied, Kind: FrameKindBatch, entries: entries}
	default:
		return TransportFrame{Raw: copied, Kind: FrameKindMalformed, err: NewInvalidRequest(nil)}
	}
}

func parseBatchEntry(raw json.RawMessage) batchEntry {
	copied := append(json.RawMessage(nil), bytes.TrimSpace(raw)...)
	entry := batchEntry{raw: copied, err: NewInvalidRequest(nil)}
	if len(copied) == 0 || copied[0] != '{' {
		return entry
	}
	var msg anyMessage
	if err := json.Unmarshal(copied, &msg); err != nil {
		return entry
	}
	entry.msg = msg
	entry.ok = true
	entry.err = nil
	return entry
}

// Encode returns the frame as one NDJSON line, preserving Raw exactly.
func (f TransportFrame) Encode() ([]byte, error) {
	if len(f.Raw) == 0 {
		return nil, errors.New("acp: empty transport frame")
	}
	if f.Raw[len(f.Raw)-1] == '\n' {
		return append([]byte(nil), f.Raw...), nil
	}
	return append(append([]byte(nil), f.Raw...), '\n'), nil
}

// ProtocolError returns the JSON-RPC error associated with a malformed frame.
func (f TransportFrame) ProtocolError() *RequestError {
	return f.err
}

// EntryCount returns 1 for a single object, the number of batch entries, or 0
// for a malformed frame.
func (f TransportFrame) EntryCount() int {
	switch f.Kind {
	case FrameKindSingle:
		return 1
	case FrameKindBatch:
		return len(f.entries)
	default:
		return 0
	}
}

func cloneRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func requestIDFromRaw(raw json.RawMessage) *json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	id, ok := fields["id"]
	if !ok {
		return nil
	}
	copied := cloneRaw(id)
	return &copied
}

func isResponseOnlyRaw(raw json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	_, hasMethod := fields["method"]
	_, hasResult := fields["result"]
	_, hasError := fields["error"]
	return !hasMethod && (hasResult || hasError)
}

func (e batchEntry) needsReply() bool {
	if !e.ok {
		return !isResponseOnlyRaw(e.raw)
	}
	if e.msg.JSONRPC != "2.0" {
		return !isResponseOnlyRaw(e.raw)
	}
	if e.msg.ID != nil && e.msg.Method == "" {
		return false
	}
	if e.msg.Method != "" && e.msg.ID != nil {
		return true
	}
	if e.msg.Method != "" {
		return false
	}
	return true
}

func jsonRPCIDOrNull(id *json.RawMessage) *json.RawMessage {
	if id != nil {
		return id
	}
	nullID := json.RawMessage("null")
	return &nullID
}

func (e batchEntry) replyID() *json.RawMessage {
	if e.ok && e.msg.ID != nil {
		return e.msg.ID
	}
	return jsonRPCIDOrNull(requestIDFromRaw(e.raw))
}
