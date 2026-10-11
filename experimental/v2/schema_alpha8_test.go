package v2

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestAlpha8CompactionPatches(t *testing.T) {
	for _, tc := range []struct {
		fields string
		state  NullableFieldState
	}{
		{``, NullableFieldAbsent},
		{`,"summary":null,"error":null,"_meta":null`, NullableFieldNull},
		{`,"summary":[],"error":"","_meta":{}`, NullableFieldValue},
		{`,"summary":false,"error":42,"_meta":"invalid"`, NullableFieldAbsent},
		{`,"summary":false,"error":123,"_meta":[]`, NullableFieldAbsent},
	} {
		var update SessionUpdate
		if err := json.Unmarshal([]byte(`{"sessionUpdate":"compaction_update","compactionId":"c","status":"_future"`+tc.fields+`}`), &update); err != nil {
			t.Fatal(err)
		}
		patch := update.CompactionUpdate
		var standalone CompactionUpdate
		if err := json.Unmarshal([]byte(`{"compactionId":"c","status":"_future"`+tc.fields+`}`), &standalone); err != nil {
			t.Fatal(err)
		}
		if standalone.SummaryState() != tc.state || standalone.ErrorState() != tc.state || standalone.MetaState() != tc.state {
			t.Fatalf("standalone lost patch states: %+v", standalone)
		}
		if patch.SummaryState() != tc.state || patch.ErrorState() != tc.state || patch.MetaState() != tc.state {
			t.Fatalf("lost patch states: %+v", patch)
		}
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		var again SessionUpdate
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatal(err)
		}
		if again.CompactionUpdate.SummaryState() != tc.state {
			t.Fatalf("roundtrip: %s", encoded)
		}
	}
}

func TestAlpha8NestedStateNotificationPreservesUnknownPayload(t *testing.T) {
	a, b := net.Pipe()
	receiver := &alpha8Client{updates: make(chan SessionUpdate, 1)}
	client, err := NewClientSideConnection(receiver, b, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	peer := acp.NewConnection(nil, a, a)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, fields := range []string{
		`"state":"idle","stopReason":"_paused","resumeAfter":30`,
		`"state":"idle","stopReason":"future_reason","resumeAfter":30,"error":42,"payload":{"n":9007199254740993}`,
		`"state":"_future_state","pending":{"n":9007199254740993}`,
	} {
		wire := []byte(`{"sessionUpdate":"state_update",` + fields + `}`)
		// Exercise both nested standalone unions and the actual session/update
		// notification path, where flattening previously discarded extensions.
		var state StateUpdate
		if err := json.Unmarshal(wire, &state); err != nil {
			t.Fatal(err)
		}
		assertAlpha8RawFields(t, state, fields)
		if err := peer.SendNotification(ctx, ClientMethodSessionUpdate, json.RawMessage(`{"sessionId":"s","update":`+string(wire)+`}`)); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-receiver.updates:
			if got.StateUpdate == nil {
				t.Fatal("state_update was not dispatched as its known outer variant")
			}
			assertAlpha8RawFields(t, got, fields)
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var again SessionUpdate
			if err := json.Unmarshal(encoded, &again); err != nil {
				t.Fatal(err)
			}
			assertAlpha8RawFields(t, again, fields)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func assertAlpha8RawFields(t *testing.T, value any, fields string) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{`+fields+`}`), &want); err != nil {
		t.Fatal(err)
	}
	for key, raw := range want {
		if string(got[key]) != string(raw) {
			t.Fatalf("%s: got %s want %s; wire=%s", key, got[key], raw, encoded)
		}
	}
}

func TestAlpha8IdleErrorAndRecovery(t *testing.T) {
	var failure Error
	if err := json.Unmarshal([]byte(`{"code":-32603,"message":"Failed","data":{"n":9007199254740993}}`), &failure); err != nil {
		t.Fatal(err)
	}
	update := IdleErrorUpdate(&failure)
	encoded, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	var again SessionUpdate
	if err := json.Unmarshal(encoded, &again); err != nil {
		t.Fatal(err)
	}
	if again.StateUpdate == nil || again.StateUpdate.StateUpdate.Idle == nil || again.StateUpdate.StateUpdate.Idle.IdleStateUpdate.Error == nil || again.StateUpdate.StateUpdate.Idle.IdleStateUpdate.Error.Error == nil || again.StateUpdate.StateUpdate.Idle.IdleStateUpdate.Error.Error.Message != "Failed" {
		t.Fatalf("lost error details: %s", encoded)
	}
	for _, reason := range []string{`null`, `123`, `[]`} {
		var standalone IdleStateUpdate
		if err := json.Unmarshal([]byte(`{"stopReason":`+reason+`}`), &standalone); err != nil {
			t.Fatal(err)
		}
		if standalone.None == nil {
			t.Fatalf("malformed reason not recovered: %s", reason)
		}
		if err := json.Unmarshal([]byte(`{"sessionUpdate":"state_update","state":"idle","stopReason":`+reason+`,"error":false}`), &again); err != nil {
			t.Fatal(err)
		}
		if again.StateUpdate.StateUpdate.Idle.IdleStateUpdate.None == nil {
			t.Fatalf("malformed details retained: %+v", again.StateUpdate)
		}
	}
	for _, input := range []string{
		`{"cwd":"/","mcpServers":[null]}`,
		`{"cwd":"/","additionalDirectories":[null]}`,
	} {
		var request NewSessionRequest
		if json.Unmarshal([]byte(input), &request) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	var resume ResumeSessionRequest
	if json.Unmarshal([]byte(`{"sessionId":"s","cwd":"/","replayFrom":"start"}`), &resume) == nil {
		t.Fatal("accepted malformed replay cursor")
	}
	var request NewSessionRequest
	if err := json.Unmarshal([]byte(`{"cwd":"/","mcpServers":[{"type":"_future","payload":{"n":9007199254740993}}]}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.McpServers[0].Other == nil {
		t.Fatal("lost unknown v2 MCP transport")
	}
}

func TestAlpha8IdleHelperReasons(t *testing.T) {
	for _, reason := range []string{"end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled", "error", "_paused", "future", "", "_quoted\"\n\x00"} {
		update := IdleUpdate(reason)
		encoded, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := json.Unmarshal(fields["stopReason"], &got); err != nil || got != reason {
			t.Fatalf("helper reason=%q wire=%s err=%v", reason, encoded, err)
		}
		if err := update.Validate(); err != nil {
			t.Fatalf("helper reason=%q: %v", reason, err)
		}
	}
}

type alpha8Client struct{ updates chan SessionUpdate }

func (*alpha8Client) RequestPermission(context.Context, RequestPermissionRequest) (RequestPermissionResponse, error) {
	return RequestPermissionResponse{}, nil
}
func (c *alpha8Client) SessionUpdate(_ context.Context, n UpdateSessionNotification) error {
	c.updates <- n.Update
	return nil
}

func TestAlpha8TypedUpdatesLoopback(t *testing.T) {
	a, b := net.Pipe()
	receiver := &alpha8Client{updates: make(chan SessionUpdate, 4)}
	agent, err := NewAgentSideConnection(nil, a, a)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = agent.Close() }()
	client, err := NewClientSideConnection(receiver, b, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, update := range []SessionUpdate{
		{Notice: &SessionUpdateNotice{Severity: NoticeSeverityInfo, Title: "Notice"}},
		{CompactionUpdate: &SessionCompactionUpdate{CompactionId: "c", Status: CompactionStatusInProgress}},
		{CompactionSummaryChunk: &SessionUpdateCompactionSummaryChunk{CompactionId: "c", Content: TextBlock("summary")}},
		IdleErrorUpdate(nil),
	} {
		if err := agent.SessionUpdate(ctx, UpdateSessionNotification{SessionId: "s", Update: update}); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-receiver.updates:
			wantJSON, _ := json.Marshal(update)
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("got %s want %s", gotJSON, wantJSON)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestAlpha8CompactionRecoveryNotification(t *testing.T) {
	a, b := net.Pipe()
	receiver := &alpha8Client{updates: make(chan SessionUpdate, 1)}
	client, err := NewClientSideConnection(receiver, b, b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	peer := acp.NewConnection(nil, a, a)
	defer func() { _ = peer.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, tc := range []struct {
		fields string
		states [3]NullableFieldState
	}{
		{`"summary":[{"type":"text","text":"retained"}],"error":"retained","_meta":{"n":9007199254740993}`, [3]NullableFieldState{NullableFieldValue, NullableFieldValue, NullableFieldValue}},
		{`"summary":false,"error":42,"_meta":"invalid"`, [3]NullableFieldState{NullableFieldAbsent, NullableFieldAbsent, NullableFieldAbsent}},
		{`"summary":false,"error":null,"_meta":{}`, [3]NullableFieldState{NullableFieldAbsent, NullableFieldNull, NullableFieldValue}},
		{`"summary":null,"error":null,"_meta":null`, [3]NullableFieldState{NullableFieldNull, NullableFieldNull, NullableFieldNull}},
	} {
		wire := json.RawMessage(`{"sessionId":"s","update":{"sessionUpdate":"compaction_update","compactionId":"c","status":"failed",` + tc.fields + `}}`)
		if err := peer.SendNotification(ctx, ClientMethodSessionUpdate, wire); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-receiver.updates:
			patch := got.CompactionUpdate
			if patch == nil {
				t.Fatal("compaction update not dispatched")
			}
			states := [3]NullableFieldState{patch.SummaryState(), patch.ErrorState(), patch.MetaState()}
			if states != tc.states {
				t.Fatalf("patch states=%v want=%v; input=%s", states, tc.states, wire)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			for i, key := range []string{"summary", "error", "_meta"} {
				raw, present := fields[key]
				switch tc.states[i] {
				case NullableFieldAbsent:
					if present {
						t.Fatalf("malformed %s became a patch: %s", key, encoded)
					}
				case NullableFieldNull:
					if string(raw) != "null" {
						t.Fatalf("explicit clear lost for %s: %s", key, encoded)
					}
				case NullableFieldValue:
					if !present || string(raw) == "null" {
						t.Fatalf("replacement lost for %s: %s", key, encoded)
					}
				}
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
