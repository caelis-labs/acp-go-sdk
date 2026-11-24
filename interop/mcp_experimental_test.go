package interop_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
	"github.com/caelis-labs/acp-go-sdk/internal/interoptest/contract"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

func TestExperimentalMCPOverACP(t *testing.T) {
	requireInterop(t)
	passed := []string{}
	for _, version := range []int{1, 2} {
		for _, probe := range []struct {
			name string
			run  func(*testing.T, int)
		}{
			{"typescript-bridge-to-go-provider", testMCPBridge},
			{"go-consumer-to-typescript-provider", func(t *testing.T, version int) {
				testMCPAdmission(t, version, stdio.Command{Executable: "node",
					Args: []string{filepath.Join(repositoryRoot, "interop/peers/typescript/dist/mcp_admission.js"), strconv.Itoa(version)}})
			}},
			{"go-consumer-to-rust-provider", func(t *testing.T, version int) {
				testMCPAdmission(t, version, stdio.Command{
					Executable: filepath.Join(repositoryRoot, "interop/peers/rust/target/debug/mcp_admission"),
					Args:       []string{strconv.Itoa(version)},
				})
			}},
		} {
			name := fmt.Sprintf("v%d/%s", version, probe.name)
			t.Run(name, func(t *testing.T) {
				probe.run(t, version)
				if !t.Failed() && !t.Skipped() {
					passed = append(passed, name)
				}
			})
		}
	}
	if len(passed) != 6 {
		t.Errorf("experimental MCP matrix incomplete: %d/6", len(passed))
	}
	if out := os.Getenv(interopEvidenceDir); out != "" {
		report := map[string]any{
			"formatVersion": 1, "passed": !t.Failed(), "matrixComplete": len(passed) == 6,
			"goCommit":     commandOutput(repositoryRoot, "git", "rev-parse", "HEAD"),
			"dirty":        commandOutput(repositoryRoot, "git", "status", "--porcelain") != "",
			"stableSchema": acp.SchemaTag, "draftSchema": v2.SchemaTag, "cases": passed,
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "mcp-experimental-evidence.json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func mcpPeer(t *testing.T, version int, command stdio.Command, handler acp.MethodHandler) (context.Context, *acp.Connection, *lockedBuffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	stderr := new(lockedBuffer)
	command.Dir = repositoryRoot
	command.Stderr = stderr
	process, err := stdio.Start(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Close() })
	opts := acp.ConnectionOptions{RejectBatch: version == 2}
	if version == 2 {
		opts.AcceptRequestID = v2.IsValidRequestID
	}
	conn, err := acp.NewConnectionWithOptions(handler, process.Input(), process.Output(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return ctx, conn, stderr
}

func mcpFixtureCapabilities(version int) contract.Object {
	if version == 1 {
		return contract.Object{"protocolVersion": float64(1), "agentCapabilities": contract.Object{"mcpCapabilities": contract.Object{"acp": true}}}
	}
	return contract.Object{
		"protocolVersion": float64(2), "info": contract.Object{"name": "mcp-fixture", "version": "1"},
		"capabilities": contract.Object{"session": contract.Object{"mcp": contract.Object{"acp": contract.Object{}}}},
	}
}

func testMCPBridge(t *testing.T, version int) {
	var mu sync.Mutex
	var forwarded, updates []string
	var serviceRuns int
	observed := make(chan struct{})
	handler := func(ctx context.Context, method string, raw json.RawMessage) (any, *acp.RequestError) {
		if method == "_interop/mcp-observed" {
			select {
			case <-observed:
				return contract.Object{}, nil
			case <-ctx.Done():
				return nil, acp.NewInternalError("child tool announcement wasn't observed")
			}
		}
		var p contract.Object
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, acp.NewInvalidParams(err.Error())
		}
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "session/update":
			update, _ := p["update"].(map[string]any)
			switch {
			case p["sessionId"] == "parent" && update["sessionUpdate"] == "subagent_update" && update["sessionId"] == "child":
				updates = append(updates, "child")
			case p["sessionId"] == "child" && update["toolCallId"] == "tool-1" &&
				(update["sessionUpdate"] == "tool_call" || update["sessionUpdate"] == "tool_call_update"):
				updates = append(updates, "tool-1")
				if slices.Equal(updates, []string{"child", "tool-1"}) {
					close(observed)
				}
			}
			return nil, nil
		case "mcp/message":
			inner, _ := p["params"].(map[string]any)
			name, _ := inner["name"].(string)
			forwarded = append(forwarded, fmt.Sprintf("%s:%v:%v", name, p["sessionId"], p["toolCallId"]))
			if p["serverId"] != "fixture" || p["method"] != "tools/call" || p["sessionId"] != "child" ||
				!slices.Equal(updates, []string{"child", "tool-1"}) {
				return nil, acp.NewInvalidParams("outside the fixture session")
			}
			serviceRuns++
			return contract.Object{"result": contract.Object{"sessionId": p["sessionId"], "toolCallId": p["toolCallId"]}}, nil
		default:
			return nil, acp.NewMethodNotFound(method)
		}
	}
	ctx, conn, stderr := mcpPeer(t, version, stdio.Command{Executable: "node",
		Args: []string{filepath.Join(repositoryRoot, "interop/peers/typescript/dist/mcp_bridge.js"), strconv.Itoa(version)}}, handler)
	init := contract.Object{"protocolVersion": version}
	if version == 2 {
		init["info"] = contract.Object{"name": "go-mcp-fixture", "version": "1"}
	} else {
		init["clientCapabilities"] = contract.Object{"subagents": contract.Object{}}
	}
	send := func(method string, params, want contract.Object) {
		t.Helper()
		got, err := acp.SendRequest[any](conn, ctx, method, params)
		if err != nil {
			t.Fatalf("%s: %v\nstderr: %s", method, err, stderr.String())
		}
		if err := contract.Match(want, got); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	capabilities := mcpFixtureCapabilities(version)
	delete(capabilities, "info") // Peer implementations use their own identity.
	send("initialize", init, capabilities)
	send("session/new", contract.Object{"cwd": "/tmp", "mcpServers": []any{
		contract.Object{"type": "acp", "name": "attribution-fixture", "serverId": "fixture"},
	}}, contract.Object{"sessionId": "parent"})
	send("_interop/mcp-bridge", contract.Object{}, contract.Object{"aborted": true, "recovered": true})
	mu.Lock()
	defer mu.Unlock()
	want := []string{"missing-session:<nil>:<nil>", "wrong-session:parent:tool-1", "owner:child:tool-1"}
	if !slices.Equal(forwarded, want) || serviceRuns != 1 {
		t.Fatalf("forwarded = %v, admitted = %d, want %v and one admitted call", forwarded, serviceRuns, want)
	}
}

func testMCPAdmission(t *testing.T, version int, command stdio.Command) {
	type setup struct {
		McpServers []struct {
			Type     string `json:"type"`
			ServerID string `json:"serverId"`
		} `json:"mcpServers"`
	}
	id := make(chan string, 1)
	ready := make(chan struct{}, 1)
	observed := make(chan struct{}, 1)
	handler := func(_ context.Context, method string, raw json.RawMessage) (any, *acp.RequestError) {
		switch method {
		case "initialize":
			return mcpFixtureCapabilities(version), nil
		case "session/new":
			var p setup
			if json.Unmarshal(raw, &p) != nil || len(p.McpServers) != 1 ||
				p.McpServers[0].Type != "acp" || p.McpServers[0].ServerID == "" {
				return nil, acp.NewInvalidParams("expected an ACP MCP declaration")
			}
			select {
			case id <- p.McpServers[0].ServerID:
			default:
				return nil, acp.NewInvalidParams("duplicate session setup")
			}
			return contract.Object{"sessionId": "parent"}, nil
		case "_interop/mcp-ready":
			select {
			case ready <- struct{}{}:
			default:
			}
			return nil, nil
		case "_interop/child-observed":
			select {
			case observed <- struct{}{}:
			default:
			}
			return nil, nil
		default:
			return nil, acp.NewMethodNotFound(method)
		}
	}
	ctx, conn, stderr := mcpPeer(t, version, command, handler)
	select {
	case <-ready:
	case <-conn.Done():
		t.Fatalf("provider closed before setup: %v\nstderr: %s", conn.Err(), stderr.String())
	case <-ctx.Done():
		t.Fatalf("provider didn't finish setup: %v\nstderr: %s", ctx.Err(), stderr.String())
	}
	var serverID string
	select {
	case serverID = <-id:
	default:
		t.Fatal("provider reported ready before declaring its server")
	}
	announced := false
	for i, probe := range []struct {
		name   string
		fields contract.Object
		want   contract.Object
	}{
		{"missing session", contract.Object{}, nil},
		{"null session", contract.Object{"sessionId": nil}, nil},
		{"tool alone", contract.Object{"toolCallId": "tool-1"}, nil},
		{"wrong session with same tool", contract.Object{"sessionId": "parent", "toolCallId": "tool-1"}, nil},
		{"unannounced child", contract.Object{"sessionId": "child"}, nil},
		{"child without tool", contract.Object{"sessionId": "child"}, contract.Object{"sessionId": "child", "toolCallId": nil, "runs": float64(1)}},
		{"child and an unreported tool", contract.Object{"sessionId": "child", "toolCallId": "tool-1"}, contract.Object{"sessionId": "child", "toolCallId": "tool-1", "runs": float64(2)}},
		{"next call with same tool", contract.Object{"sessionId": "child", "toolCallId": "tool-1"}, contract.Object{"sessionId": "child", "toolCallId": "tool-1", "runs": float64(3)}},
	} {
		if probe.want != nil && !announced {
			err := conn.SendNotification(ctx, "session/update", contract.Object{
				"sessionId": "parent",
				"update":    contract.Object{"sessionUpdate": "subagent_update", "sessionId": "child"},
			})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-observed:
				announced = true
			case <-conn.Done():
				t.Fatalf("provider closed before observing child: %v\nstderr: %s", conn.Err(), stderr.String())
			case <-ctx.Done():
				t.Fatalf("provider didn't observe child: %v\nstderr: %s", ctx.Err(), stderr.String())
			}
		}
		params := contract.Object{
			"serverId": serverID, "requestId": fmt.Sprintf("call-%d", i), "method": "tools/call",
			"params": contract.Object{"name": "fixture", "_meta": contract.Object{
				"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": contract.Object{},
			}},
		}
		for key, value := range probe.fields {
			params[key] = value
		}
		got, err := acp.SendRequest[any](conn, ctx, "mcp/message", params)
		if probe.want == nil {
			var remote *acp.RequestError
			if !errors.As(err, &remote) || remote.Code != -32602 {
				t.Fatalf("%s: expected outer Invalid params, got %v, %v", probe.name, got, err)
			}
		} else {
			if err != nil {
				t.Fatalf("%s: %v\nstderr: %s", probe.name, err, stderr.String())
			}
			if err := contract.Match(contract.Object{"result": probe.want}, got); err != nil {
				t.Fatalf("%s: %v", probe.name, err)
			}
		}
	}
}
