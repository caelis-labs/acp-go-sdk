package v2

import (
	"bufio"
	"encoding/json"
	"fmt"
	acp "github.com/caelis-labs/acp-go-sdk"
	"net"
	"testing"
	"time"
)

func TestV2InvalidRequestIDUsesNullErrorID(t *testing.T) {
	for _, raw := range []string{"1.5", "null", "9223372036854775808"} {
		t.Run(raw, func(t *testing.T) {
			agentIO, clientIO := net.Pipe()
			defer func() { _ = clientIO.Close() }()
			_ = clientIO.SetDeadline(time.Now().Add(5 * time.Second))
			agent, err := NewAgentSideConnection(&loopbackAgent{}, agentIO, agentIO)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = agent.Close() }()
			if _, err := fmt.Fprintf(clientIO, "{\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"initialize\",\"params\":{\"protocolVersion\":2,\"info\":{\"name\":\"test\",\"version\":\"1\"}}}\n", raw); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(clientIO)
			if !scanner.Scan() {
				t.Fatal(scanner.Err())
			}
			var response struct {
				ID    json.RawMessage `json:"id"`
				Error struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if string(response.ID) != "null" || response.Error.Code != -32600 {
				t.Fatalf("invalid response: %s", scanner.Bytes())
			}
		})
	}
}

func TestV2IDValidatorRetainsLargeIntegerPrecision(t *testing.T) {
	for _, raw := range []string{`"request"`, `0`, `9007199254740993`, `-9223372036854775808`, `9223372036854775807`} {
		if !IsValidRequestID(json.RawMessage(raw)) {
			t.Fatalf("rejected valid ID %s", raw)
		}
	}
}

func TestV2IDAdmissionComposesCallerPredicate(t *testing.T) {
	opts := protocolConnectionOptions(acp.ConnectionOptions{AcceptRequestID: func(raw json.RawMessage) bool { return string(raw) == `"allowed"` }})
	if !opts.RejectBatch || !opts.AcceptRequestID(json.RawMessage(`"allowed"`)) {
		t.Fatal("lost valid v2 admission")
	}
	for _, raw := range []string{`"blocked"`, `1.5`, `null`} {
		if opts.AcceptRequestID(json.RawMessage(raw)) {
			t.Fatalf("accepted %s", raw)
		}
	}
	permissive := protocolConnectionOptions(acp.ConnectionOptions{AcceptRequestID: func(json.RawMessage) bool { return true }})
	if permissive.AcceptRequestID(json.RawMessage(`1.5`)) {
		t.Fatal("caller predicate weakened v2 constraints")
	}
}
