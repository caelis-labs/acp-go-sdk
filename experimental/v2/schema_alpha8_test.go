package v2

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestAlpha8CompactionPatches(t *testing.T) {
	for _, tc := range []struct {
		fields string
		state  NullableFieldState
	}{
		{``, NullableFieldAbsent},
		{`,"summary":null,"error":null,"_meta":null`, NullableFieldNull},
		{`,"summary":[],"error":"","_meta":{}`, NullableFieldValue},
		{`,"summary":false,"error":123,"_meta":[]`, NullableFieldNull},
	} {
		var update SessionUpdate
		if err := json.Unmarshal([]byte(`{"sessionUpdate":"compaction_update","compactionId":"c","status":"_future"`+tc.fields+`}`), &update); err != nil {
			t.Fatal(err)
		}
		patch := update.CompactionUpdate
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
	if again.StateUpdate == nil || again.StateUpdate.Error == nil || again.StateUpdate.Error.Message != "Failed" || *again.StateUpdate.StopReason != "error" {
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
		if again.StateUpdate.StopReason != nil || again.StateUpdate.Error != nil {
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
