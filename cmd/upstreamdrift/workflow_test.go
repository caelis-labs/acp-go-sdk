package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual workflow entry point, including the process exit code
// and issue commands. The GitHub API and CLI are local fakes; no issue is sent.
func TestWorkflowCreatesIssueOnDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("workflow runs on Linux")
	}
	root := repoRoot(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "schema-v1"):
			_ = json.NewEncoder(w).Encode([]githubRef{{Ref: "refs/tags/schema-v1.21.0"}})
		case strings.Contains(r.URL.Path, "schema-v2"):
			_ = json.NewEncoder(w).Encode([]githubRef{{Ref: "refs/tags/schema-v2.0.0-alpha.3"}})
		case strings.Contains(r.URL.Path, "typescript-sdk"):
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v1.4.0"})
		case strings.Contains(r.URL.Path, "rust-sdk"):
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v2.2.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	temp := t.TempDir()
	stub := `#!/usr/bin/env bash
printf '%s\n' "$*" >> "$RUNNER_TEMP/gh-calls"
if [[ "$1 $2" == "issue create" ]]; then echo 'https://example.invalid/issues/1'; fi
`
	if err := os.WriteFile(filepath.Join(temp, "gh"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts", "upstream-drift.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+temp+string(os.PathListSeparator)+os.Getenv("PATH"), "RUNNER_TEMP="+temp, "ACP_UPSTREAM_GITHUB_API="+server.URL, "GITHUB_TOKEN=", "GH_TOKEN=")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("workflow exit=%v output=%s", err, output)
	}
	calls, err := os.ReadFile(filepath.Join(temp, "gh-calls"))
	if err != nil {
		t.Fatalf("issue command not reached: %v\n%s", err, output)
	}
	if !strings.Contains(string(calls), "issue create") {
		t.Fatalf("calls=%s output=%s", calls, output)
	}
	raw, err := os.ReadFile(filepath.Join(temp, "upstream-drift.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got report
	if err := json.Unmarshal(raw, &got); err != nil || got.Status != "drift" {
		t.Fatalf("report=%s err=%v", raw, err)
	}
}
