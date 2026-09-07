package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommittedLockIsConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	lock, err := loadLock(filepath.Join(root, "upstream", "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkLocal(root, lock); err != nil {
		t.Fatal(err)
	}
	if lock.RustSDK.Tag != "v2.1.0" {
		t.Fatalf("rust pin = %q, want v2.1.0", lock.RustSDK.Tag)
	}
}

func TestCheckLocalRejectsMismatchedPins(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "upstream", "lock.json"), validLock())
	writeJSON(t, filepath.Join(root, "schema", "lock.json"), map[string]string{
		"repository": "https://github.com/agentclientprotocol/agent-client-protocol",
		"tag":        "schema-v1.20.0",
		"commit":     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	writeJSON(t, filepath.Join(root, "interop", "versions.json"), map[string]any{
		"typescript": map[string]string{
			"repository": "https://github.com/agentclientprotocol/typescript-sdk",
			"tag":        "v1.4.0",
			"commit":     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		"rust": map[string]string{
			"repository": "https://github.com/agentclientprotocol/rust-sdk",
			"tag":        "v2.1.0",
			"commit":     "cccccccccccccccccccccccccccccccccccccccc",
		},
	})
	lock, err := loadLock(filepath.Join(root, "upstream", "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkLocal(root, lock); err == nil {
		t.Fatal("expected schema lock mismatch")
	}
}

func TestCheckRemoteReportsNewerRustRelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/matching-refs/tags/schema-v1"):
			writeJSONValue(t, w, []githubRef{{Ref: "refs/tags/schema-v1.21.0"}})
		case strings.Contains(r.URL.Path, "/matching-refs/tags/schema-v2"):
			writeJSONValue(t, w, []githubRef{{Ref: "refs/tags/schema-v2.0.0-alpha.3"}})
		case strings.Contains(r.URL.Path, "/typescript-sdk/releases/latest"):
			writeJSONValue(t, w, githubRelease{TagName: "v1.4.0"})
		case strings.Contains(r.URL.Path, "/rust-sdk/releases/latest"):
			writeJSONValue(t, w, githubRelease{TagName: "v2.2.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	findings, err := checkRemote(ctx, &githubClient{http: server.Client(), baseURL: server.URL}, validLock())
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Source != "rustSdk" || findings[0].Latest != "v2.2.0" {
		t.Fatalf("findings = %+v, want rust v2.2.0 only", findings)
	}
	rep := buildReport(findings)
	if rep.Status != "drift" {
		t.Fatalf("status = %q, want drift", rep.Status)
	}
	if !strings.Contains(rep.IssueTitle, "rust-sdk v2.2.0 available; pinned v2.1.0") {
		t.Fatalf("issue title = %q", rep.IssueTitle)
	}
}

func TestCheckRemoteCurrent(t *testing.T) {
	t.Parallel()
	lock := validLock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/matching-refs/tags/schema-v1"):
			writeJSONValue(t, w, []githubRef{
				{Ref: "refs/tags/schema-v1.9.0"},
				{Ref: "refs/tags/schema-v1.21.0"},
			})
		case strings.Contains(r.URL.Path, "/matching-refs/tags/schema-v2"):
			writeJSONValue(t, w, []githubRef{
				{Ref: "refs/tags/schema-v2.0.0-alpha.2"},
				{Ref: "refs/tags/schema-v2.0.0-alpha.3"},
			})
		case strings.Contains(r.URL.Path, "/typescript-sdk/releases/latest"):
			writeJSONValue(t, w, githubRelease{TagName: lock.TypeScriptSDK.Tag})
		case strings.Contains(r.URL.Path, "/rust-sdk/releases/latest"):
			writeJSONValue(t, w, githubRelease{TagName: lock.RustSDK.Tag})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	findings, err := checkRemote(ctx, &githubClient{http: server.Client(), baseURL: server.URL}, lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestIssueTitleMultipleFindings(t *testing.T) {
	t.Parallel()
	title := issueTitle([]finding{
		{Name: "rust-sdk", Latest: "v2.2.0"},
		{Name: "typescript-sdk", Latest: "v1.5.0"},
	})
	if title != "upstream-drift: rust-sdk v2.2.0, typescript-sdk v1.5.0 available" {
		t.Fatalf("title = %q", title)
	}
}

func validLock() upstreamLock {
	return upstreamLock{
		Protocol: protocolLock{
			Repository:   "https://github.com/agentclientprotocol/agent-client-protocol",
			StableTag:    "schema-v1.21.0",
			StableCommit: "272bf799f35a258c6a4107a0410ed361e83683d3",
			V2Tag:        "schema-v2.0.0-alpha.3",
			V2Commit:     "272bf799f35a258c6a4107a0410ed361e83683d3",
		},
		TypeScriptSDK: sdkLock{
			Repository: "https://github.com/agentclientprotocol/typescript-sdk",
			Tag:        "v1.4.0",
			Commit:     "e6463f444093ed7c5f1cc937c3f32afb5853e906",
		},
		RustSDK: sdkLock{
			Repository: "https://github.com/agentclientprotocol/rust-sdk",
			Tag:        "v2.1.0",
			Commit:     "726c5030bfaa88cfdac2fb1f71a63abb331ce586",
		},
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONValue(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
