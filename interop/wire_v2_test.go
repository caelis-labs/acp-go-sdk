package interop_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/caelis-labs/acp-go-sdk/internal/interoptest/contract"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

// Invalid envelopes must be sent below the SDK client API: official v2 clients
// pin outgoing initialize to v2 and reject/canonicalize invalid IDs or arrays.
func TestV2WireRejections(t *testing.T) {
	requireInterop(t)
	binary := filepath.Join(t.TempDir(), "contract-go-agent")
	build := exec.Command("go", "build", "-o", binary, "./internal/interoptest/contractagent")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	init := `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":2,"info":{"name":"wire-probe","version":"1"}}}`
	type probe struct {
		name, frame string
		initialize  bool
		code        int
	}
	probes := []probe{
		{"before-initialize", `{"jsonrpc":"2.0","id":"p","method":"session/new","params":{"cwd":"/tmp"}}`, false, -32600},
		{"lower-version", `{"jsonrpc":"2.0","id":"p","method":"initialize","params":{"protocolVersion":1}}`, false, -32022},
		{"batch", `[{"jsonrpc":"2.0","id":"p","method":"session/list","params":{}}]`, true, -32600},
		{"fractional-id", `{"jsonrpc":"2.0","id":1.5,"method":"session/list","params":{}}`, true, -32600},
		{"null-id", `{"jsonrpc":"2.0","id":null,"method":"session/list","params":{}}`, true, -32600},
		{"no-load", `{"jsonrpc":"2.0","id":"p","method":"session/load","params":{"sessionId":"s1","cwd":"/tmp"}}`, true, -32601},
		{"unknown-method", `{"jsonrpc":"2.0","id":"p","method":"unknown/method","params":{}}`, true, -32601},
	}
	evidence := []map[string]any{}
	for _, peer := range []string{"go", "typescript", "rust"} {
		for _, probe := range probes {
			t.Run(peer+"/"+probe.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var stderr lockedBuffer
				executable := binary
				args := []string{"-version", "2", "-profile", "full", "-updates", filepath.Join(repositoryRoot, "interop/contract/updates.json")}
				if peer != "go" {
					args = []string{"--version", "2", "--profile", "full", "--updates", filepath.Join(repositoryRoot, "interop/contract/updates.json"), "--role", "agent"}
					if peer == "typescript" {
						executable = "node"
						args = append([]string{filepath.Join(repositoryRoot, "interop/peers/typescript/dist/contract.js")}, args...)
					} else {
						executable = filepath.Join(repositoryRoot, "interop/peers/rust/target/debug/contract")
					}
				}
				p, err := stdio.Start(ctx, stdio.Command{Executable: executable, Args: args, Dir: repositoryRoot, Stderr: &stderr})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = p.Close() }()
				scanner := bufio.NewScanner(p.Output())
				scanner.Buffer(make([]byte, 4096), 65536)
				exchange := func(frame string) map[string]any {
					t.Helper()
					if _, err := fmt.Fprintln(p.Input(), frame); err != nil {
						t.Fatal(err)
					}
					if !scanner.Scan() {
						t.Fatalf("no response: %v %s", scanner.Err(), stderr.String())
					}
					var result map[string]any
					if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
						t.Fatalf("expected one response object: %s", scanner.Bytes())
					}
					return result
				}
				if probe.initialize {
					r := exchange(init)
					if err := contract.Match(map[string]any{"result": map[string]any{"protocolVersion": float64(2)}}, r); err != nil {
						t.Fatal(err)
					}
				}
				response := exchange(probe.frame)
				if err := contract.Match(map[string]any{"error": map[string]any{"code": float64(probe.code)}}, response); err != nil {
					t.Fatalf("%v: %s", err, stderr.String())
				}
				if probe.name == "lower-version" {
					if err := contract.Match(map[string]any{"error": map[string]any{"data": map[string]any{"requested": float64(1), "supported": []any{float64(2)}}}}, response); err != nil {
						t.Fatal(err)
					}
				}
				if probe.name == "batch" || probe.name == "fractional-id" || probe.name == "null-id" {
					if id, ok := response["id"]; !ok || id != nil {
						t.Fatalf("invalid envelope response id=%v", id)
					}
				}
				evidence = append(evidence, map[string]any{"peer": peer, "probe": probe.name, "code": probe.code, "response": response})
			})
		}
	}
	out := os.Getenv(interopEvidenceDir)
	if out != "" {
		report := map[string]any{"passed": !t.Failed(), "matrixComplete": len(evidence) == len(probes)*3, "goCommit": commandOutput(repositoryRoot, "git", "rev-parse", "HEAD"), "dirty": commandOutput(repositoryRoot, "git", "status", "--porcelain") != "", "cases": evidence}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "wire-v2-evidence.json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
