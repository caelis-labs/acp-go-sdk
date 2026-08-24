package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func FuzzRequestID(f *testing.F) {
	for _, seed := range []string{"1", "-1", "1e3", "1.2500", `"request-id"`, "null", "9223372036854775807"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		key, err := canonicalJSONRPCIDKey(input)
		if err != nil {
			return
		}
		if !json.Valid([]byte(key)) {
			t.Fatalf("canonical key is not JSON: %q", key)
		}
		if second, err := canonicalJSONRPCIDKey(json.RawMessage(key)); err != nil || second != key {
			t.Fatalf("canonicalization is not idempotent: %q, %v", second, err)
		}
	})
}

func FuzzGeneratedUnions(f *testing.F) {
	for _, seed := range []string{
		`{"type":"text","text":"hello"}`,
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}`,
		`{"mode":"form","message":"m","requestedSchema":{"type":"object","properties":{}}}`,
		`null`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(_ *testing.T, input []byte) {
		for _, target := range []any{
			new(ContentBlock),
			new(SessionUpdate),
			new(CreateElicitationRequest),
			new(CreateElicitationResponse),
			new(RequestId),
		} {
			if json.Unmarshal(input, target) == nil {
				_, _ = json.Marshal(target)
			}
		}
	})
}

func FuzzConnectionFraming(f *testing.F) {
	for _, seed := range []string{
		"",
		"\n",
		`{"jsonrpc":"2.0","method":"note"}` + "\n",
		`{"jsonrpc":"2.0","id":"x","method":"call"}` + "\n",
		"{\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		var output bytes.Buffer
		opts := ConnectionOptions{
			MaxFrameSize:           512,
			MaxPendingRequests:     2,
			MaxHandlerConcurrency:  1,
			MaxQueuedRequests:      2,
			MaxQueuedNotifications: 2,
			MaxQueuedWrites:        2,
		}
		connection, err := NewConnectionWithOptions(
			func(context.Context, string, json.RawMessage) (any, *RequestError) { return nil, nil },
			&output,
			bytes.NewReader(input),
			opts,
		)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = connection.Wait(ctx)
		_ = connection.Close()
	})
}
