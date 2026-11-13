package interop_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
	"github.com/caelis-labs/acp-go-sdk/internal/interoptest/contract"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

type contractEvidence struct {
	Case      string   `json:"case"`
	Direction string   `json:"direction"`
	Version   int      `json:"version"`
	Trace     []string `json:"trace"`
	Reverse   []string `json:"reverse,omitempty"`
}

func TestContractMatrix(t *testing.T) {
	requireInterop(t)
	directory := filepath.Join(repositoryRoot, "interop", "contract")
	paths, err := filepath.Glob(filepath.Join(directory, "v*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(contractCaseNames) {
		t.Fatalf("expected full contract case set, got %d", len(paths))
	}
	binary := filepath.Join(t.TempDir(), "contract-go-agent")
	build := exec.Command("go", "build", "-o", binary, "./internal/interoptest/contractagent")
	build.Dir = repositoryRoot
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	evidence := []contractEvidence{}
	for _, path := range paths {
		var c contract.Case
		if err := contract.Read(path, &c); err != nil {
			t.Fatal(err)
		}
		for _, peer := range []string{"typescript", "rust"} {
			args := []string{}
			executable := ""
			if peer == "typescript" {
				executable = "node"
				args = append(args, filepath.Join(repositoryRoot, "interop/peers/typescript/dist/contract.js"))
			} else {
				executable = filepath.Join(repositoryRoot, "interop/peers/rust/target/debug/contract")
			}
			common := []string{"--version", fmt.Sprint(c.Version), "--profile", c.Profile, "--updates", filepath.Join(directory, "updates.json")}
			for _, role := range []string{"agent", "client"} {
				key := c.Name + "/" + peer + "-" + role
				t.Run(key, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
					defer cancel()
					var stderr lockedBuffer
					var trace []string
					var reverseTrace []string
					if role == "agent" {
						command := stdio.Command{Executable: executable, Args: append(append(append([]string{}, args...), common...), "--role", "agent"), Dir: repositoryRoot, Stderr: &stderr}
						p, err := stdio.Start(ctx, command)
						if err != nil {
							t.Fatal(err)
						}
						defer func() { _ = p.Close() }()
						var mu sync.Mutex
						updates := []contract.Object{}
						handler := func(_ context.Context, method string, raw json.RawMessage) (any, *acp.RequestError) {
							if method == "session/request_permission" || method == "elicitation/create" || method == "elicitation/complete" {
								var vectors []struct {
									Method   string
									Params   any
									Response any
								}
								if err := contract.Read(filepath.Join(directory, "reverse.json"), &vectors); err != nil {
									return nil, acp.NewInternalError(err.Error())
								}
								var params any
								if err := json.Unmarshal(raw, &params); err != nil {
									return nil, acp.NewInvalidParams(err.Error())
								}
								for _, v := range vectors {
									if v.Method == method {
										if err := contract.Match(v.Params, params); err != nil {
											continue
										}
										mu.Lock()
										reverseTrace = append(reverseTrace, method)
										mu.Unlock()
										return v.Response, nil
									}
								}
							}

							if method == "session/update" {
								if c.Version == 2 {
									var typed v2.UpdateSessionNotification
									if err := json.Unmarshal(raw, &typed); err != nil {
										return nil, acp.NewInvalidParams(err.Error())
									}
								}
								var n struct {
									Update contract.Object `json:"update"`
								}
								if err := json.Unmarshal(raw, &n); err != nil {
									return nil, acp.NewInvalidParams(err.Error())
								}
								mu.Lock()
								updates = append(updates, n.Update)
								mu.Unlock()
								return nil, nil
							}
							return nil, acp.NewMethodNotFound(method)
						}
						conn, err := acp.NewConnectionWithOptions(handler, p.Input(), p.Output(), acp.ConnectionOptions{RejectBatch: c.Version == 2})
						if err != nil {
							t.Fatal(err)
						}
						defer func() { _ = conn.Close() }()
						trace, err = contract.Run(ctx, conn, c, &updates, &mu)
						if err != nil {
							t.Fatalf("%v\nstderr: %s", err, stderr.String())
						}
					} else {
						result := filepath.Join(t.TempDir(), "result.json")
						a := append(append(append([]string{}, args...), common...), "--role", "client", "--case", path, "--agent", binary, "--result", result)
						cmd := exec.CommandContext(ctx, executable, a...)
						cmd.Dir = repositoryRoot
						var stdout lockedBuffer
						cmd.Stdout = &stdout
						cmd.Stderr = &stderr
						if err := cmd.Run(); err != nil {
							t.Fatalf("%v\nstdout:%s\nstderr:%s", err, stdout.String(), stderr.String())
						}
						if stdout.String() != "" {
							t.Fatal("unexpected outer stdout")
						}
						var observed struct {
							Trace   []string `json:"trace"`
							Reverse []string `json:"reverse"`
						}
						if err := contract.Read(result, &observed); err != nil {
							t.Fatal(err)
						}
						trace = observed.Trace
						reverseTrace = observed.Reverse
					}
					if c.Name == "v2-lifecycle" || c.Name == "v2-after" || c.Name == "v2-content" {
						if len(reverseTrace) != 4 || reverseTrace[0] != "session/request_permission" || reverseTrace[1] != "elicitation/create" || reverseTrace[2] != "elicitation/create" || reverseTrace[3] != "elicitation/complete" {
							t.Fatalf("missing reverse calls: %v", reverseTrace)
						}
					}
					if len(trace) != len(c.Steps) {
						t.Fatalf("incomplete trace: %d/%d", len(trace), len(c.Steps))
					}
					for i, step := range c.Steps {
						if trace[i] != fmt.Sprintf("%d:%s:ok", i, step.Method) {
							t.Fatalf("invalid trace step %d", i)
						}
					}
					direction := "go-client-to-" + peer + "-agent"
					if role == "client" {
						direction = peer + "-client-to-go-agent"
					}
					evidence = append(evidence, contractEvidence{Case: c.Name, Version: c.Version, Direction: direction, Trace: trace, Reverse: reverseTrace})
				})
			}
		}
	}
	if len(evidence) != len(paths)*4 {
		t.Errorf("contract matrix incomplete: %d/%d", len(evidence), len(paths)*4)
	}
	fixtureHashes := map[string]string{}
	allFixtures, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range allFixtures {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fixtureHashes[filepath.Base(path)] = fmt.Sprintf("%x", sha256.Sum256(b))
	}
	versions, err := os.ReadFile(filepath.Join(repositoryRoot, "interop/versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"formatVersion": 1, "fixtureSha256": fixtureHashes, "versions": json.RawMessage(versions), "goVersion": runtime.Version(), "nodeVersion": commandOutput(repositoryRoot, "node", "--version"), "rustVersion": commandOutput(filepath.Join(repositoryRoot, "interop/peers/rust"), "rustc", "--version"), "passed": !t.Failed(), "matrixComplete": len(evidence) == len(paths)*4, "goCommit": commandOutput(repositoryRoot, "git", "rev-parse", "HEAD"), "dirty": commandOutput(repositoryRoot, "git", "status", "--porcelain") != "", "stableSchema": acp.SchemaTag, "draftSchema": v2.SchemaTag, "cases": evidence}
	out := os.Getenv(interopEvidenceDir)
	if out != "" {
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "contract-evidence.json"), append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
