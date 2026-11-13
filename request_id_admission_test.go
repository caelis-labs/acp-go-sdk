package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestIDAdmissionPreservesStableDefault(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			server, client := net.Pipe()
			defer func() { _ = client.Close() }()
			_ = client.SetDeadline(time.Now().Add(5 * time.Second))
			var dispatched atomic.Bool
			opts := ConnectionOptions{}
			if reject {
				opts.AcceptRequestID = func(json.RawMessage) bool { return false }
			}
			conn, err := NewConnectionWithOptions(func(context.Context, string, json.RawMessage) (any, *RequestError) {
				dispatched.Store(true)
				return "accepted", nil
			}, server, server, opts)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if _, err := fmt.Fprintln(client, `{"jsonrpc":"2.0","id":1.5,"method":"echo","params":{}}`); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(client)
			if !scanner.Scan() {
				t.Fatal(scanner.Err())
			}
			var response struct {
				ID     json.RawMessage `json:"id"`
				Result string          `json:"result"`
				Error  *RequestError   `json:"error"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if reject {
				if dispatched.Load() || string(response.ID) != "null" || response.Error == nil || response.Error.Code != -32600 {
					t.Fatalf("invalid admission response: %s", scanner.Bytes())
				}
			} else if !dispatched.Load() || string(response.ID) != "1.5" || response.Result != "accepted" {
				t.Fatalf("stable default changed: %s", scanner.Bytes())
			}
		})
	}
}
