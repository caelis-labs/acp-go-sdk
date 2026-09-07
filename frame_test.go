package acp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestParseTransportFrameSingleBatchAndMalformed(t *testing.T) {
	t.Parallel()

	single := ParseTransportFrame([]byte(`{"jsonrpc":"2.0","id":1,"method":"echo"}`))
	if single.Kind != FrameKindSingle || single.EntryCount() != 1 {
		t.Fatalf("single = %#v", single)
	}
	if string(single.single.Method) != "echo" {
		t.Fatalf("method = %q", single.single.Method)
	}

	batchRaw := `[{"jsonrpc":"2.0","id":1,"method":"a"},{"jsonrpc":"2.0","method":"note"},{"jsonrpc":"2.0","id":2,"method":"b"}]`
	batch := ParseTransportFrame([]byte(batchRaw))
	if batch.Kind != FrameKindBatch || batch.EntryCount() != 3 {
		t.Fatalf("batch = %#v", batch)
	}
	if !bytes.Equal(batch.Raw, []byte(batchRaw)) {
		t.Fatalf("batch raw was rewritten: %s", batch.Raw)
	}
	if !batch.entries[0].needsReply() || batch.entries[1].needsReply() || !batch.entries[2].needsReply() {
		t.Fatal("batch reply classification")
	}

	empty := ParseTransportFrame([]byte(`[]`))
	if empty.Kind != FrameKindMalformed || empty.ProtocolError() == nil || empty.ProtocolError().Code != -32600 {
		t.Fatalf("empty batch = %#v", empty)
	}

	malformed := ParseTransportFrame([]byte(`{`))
	if malformed.Kind != FrameKindMalformed || malformed.ProtocolError() == nil || malformed.ProtocolError().Code != -32700 {
		t.Fatalf("parse error = %#v", malformed)
	}

	number := ParseTransportFrame([]byte(`1`))
	if number.Kind != FrameKindMalformed || number.ProtocolError().Code != -32600 {
		t.Fatalf("number = %#v", number)
	}
}

func TestParseTransportFramePreservesMalformedBatchEntries(t *testing.T) {
	t.Parallel()
	frame := ParseTransportFrame([]byte(`[{"jsonrpc":"2.0","id":1,"method":"ok"},"nope",{"result":true,"id":2}]`))
	if frame.Kind != FrameKindBatch || frame.EntryCount() != 3 {
		t.Fatalf("frame = %#v", frame)
	}
	if !frame.entries[0].ok || !frame.entries[0].needsReply() {
		t.Fatal("first entry should be a request")
	}
	if frame.entries[1].ok || !frame.entries[1].needsReply() {
		t.Fatal("string entry should be a malformed call")
	}
	if frame.entries[2].needsReply() {
		t.Fatal("response-shaped entry should not produce a batch reply")
	}
}

func TestTransportFrameEncodePreservesRaw(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"jsonrpc":"2.0","id":1,"method":"echo"}]`)
	frame := ParseTransportFrame(raw)
	encoded, err := frame.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, append([]byte(raw), '\n')) {
		t.Fatalf("encoded = %s", encoded)
	}
}
