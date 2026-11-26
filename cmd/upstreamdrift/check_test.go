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

func TestCheckLocalRejectsStaleDocsAndSyncDocsRepairsThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lock := validLock()
	writeJSON(t, filepath.Join(root, "upstream", "lock.json"), lock)
	writeJSON(t, filepath.Join(root, "schema", "lock.json"), map[string]any{
		"repository": lock.Protocol.Repository,
		"tag":        lock.Protocol.StableTag,
		"commit":     lock.Protocol.StableCommit,
		"assets": map[string]string{
			"schema.json": "1111111111111111111111111111111111111111111111111111111111111111",
		},
	})
	writeJSON(t, filepath.Join(root, "schema", "v2", "lock.json"), map[string]any{
		"repository": lock.Protocol.Repository,
		"tag":        lock.Protocol.V2Tag,
		"commit":     lock.Protocol.V2Commit,
		"assets": map[string]string{
			"schema.json": "2222222222222222222222222222222222222222222222222222222222222222",
		},
	})
	writeJSON(t, filepath.Join(root, "interop", "versions.json"), map[string]any{
		"typescript": map[string]string{
			"package":    "@agentclientprotocol/sdk",
			"repository": lock.TypeScriptSDK.Repository,
			"tag":        lock.TypeScriptSDK.Tag,
			"commit":     lock.TypeScriptSDK.Commit,
		},
		"rust": map[string]string{
			"package":    "agent-client-protocol",
			"repository": lock.RustSDK.Repository,
			"tag":        lock.RustSDK.Tag,
			"commit":     lock.RustSDK.Commit,
			"toolchain":  "1.88.0",
		},
	})

	readme := "# ACP\n\n<!-- upstream-pins:en:start -->\nstale\n<!-- upstream-pins:en:end -->\n"
	readmeZH := "# ACP\n\n<!-- upstream-pins:zh-CN:start -->\nstale\n<!-- upstream-pins:zh-CN:end -->\n"
	v2Doc := "# v2\n\n<!-- upstream-pins:v2:start -->\nstale\n<!-- upstream-pins:v2:end -->\n"
	for rel, body := range map[string]string{
		"README.md":       readme,
		"README.zh-CN.md": readmeZH,
		filepath.Join("docs", "experimental-v2.md"): v2Doc,
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("checkLocal err = %v, want out-of-date error", err)
	}

	updated, err := syncDocs(root, lock)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 3 {
		t.Fatalf("updated = %v, want 3 files", updated)
	}
	if err := checkLocal(root, lock); err != nil {
		t.Fatalf("checkLocal after syncDocs failed: %v", err)
	}
}

func TestCheckLocalRejectsMismatchedV2SchemaLock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lock := validLock()
	writeJSON(t, filepath.Join(root, "upstream", "lock.json"), lock)
	writeJSON(t, filepath.Join(root, "schema", "lock.json"), map[string]string{
		"repository": lock.Protocol.Repository,
		"tag":        lock.Protocol.StableTag,
		"commit":     lock.Protocol.StableCommit,
	})
	writeJSON(t, filepath.Join(root, "schema", "v2", "lock.json"), map[string]string{
		"repository": lock.Protocol.Repository,
		"tag":        "schema-v2.0.0-alpha.999",
		"commit":     lock.Protocol.V2Commit,
	})
	writeJSON(t, filepath.Join(root, "interop", "versions.json"), map[string]any{
		"typescript": map[string]string{
			"repository": lock.TypeScriptSDK.Repository,
			"tag":        lock.TypeScriptSDK.Tag,
			"commit":     lock.TypeScriptSDK.Commit,
		},
		"rust": map[string]string{
			"repository": lock.RustSDK.Repository,
			"tag":        lock.RustSDK.Tag,
			"commit":     lock.RustSDK.Commit,
		},
	})
	if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "schema/v2/lock.json") {
		t.Fatalf("checkLocal err = %v, want schema/v2/lock.json mismatch", err)
	}
}

func TestCheckLocalRejectsMissingDocOrMarkersAndEmptyDigest(t *testing.T) {
	t.Parallel()
	newFixture := func(t *testing.T) (string, upstreamLock) {
		t.Helper()
		root := t.TempDir()
		lock := validLock()
		writeJSON(t, filepath.Join(root, "upstream", "lock.json"), lock)
		writeJSON(t, filepath.Join(root, "schema", "lock.json"), map[string]any{
			"repository": lock.Protocol.Repository,
			"tag":        lock.Protocol.StableTag,
			"commit":     lock.Protocol.StableCommit,
			"assets": map[string]string{
				"schema.json": "1111111111111111111111111111111111111111111111111111111111111111",
			},
		})
		writeJSON(t, filepath.Join(root, "schema", "v2", "lock.json"), map[string]any{
			"repository": lock.Protocol.Repository,
			"tag":        lock.Protocol.V2Tag,
			"commit":     lock.Protocol.V2Commit,
			"assets": map[string]string{
				"schema.json": "2222222222222222222222222222222222222222222222222222222222222222",
			},
		})
		writeJSON(t, filepath.Join(root, "interop", "versions.json"), map[string]any{
			"typescript": map[string]string{
				"package":    "@agentclientprotocol/sdk",
				"repository": lock.TypeScriptSDK.Repository,
				"tag":        lock.TypeScriptSDK.Tag,
				"commit":     lock.TypeScriptSDK.Commit,
			},
			"rust": map[string]string{
				"package":    "agent-client-protocol",
				"repository": lock.RustSDK.Repository,
				"tag":        lock.RustSDK.Tag,
				"commit":     lock.RustSDK.Commit,
				"toolchain":  "1.88.0",
			},
		})
		for rel, body := range map[string]string{
			"README.md":       "# ACP\n\n<!-- upstream-pins:en:start -->\nstale\n<!-- upstream-pins:en:end -->\n",
			"README.zh-CN.md": "# ACP\n\n<!-- upstream-pins:zh-CN:start -->\nstale\n<!-- upstream-pins:zh-CN:end -->\n",
			filepath.Join("docs", "experimental-v2.md"): "# v2\n\n<!-- upstream-pins:v2:start -->\nstale\n<!-- upstream-pins:v2:end -->\n",
		} {
			p := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := syncDocs(root, lock); err != nil {
			t.Fatal(err)
		}
		return root, lock
	}

	t.Run("missing managed doc", func(t *testing.T) {
		root, lock := newFixture(t)
		if err := os.Remove(filepath.Join(root, "README.zh-CN.md")); err != nil {
			t.Fatal(err)
		}
		if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "README.zh-CN.md") {
			t.Fatalf("checkLocal err = %v, want missing README.zh-CN.md error", err)
		}
	})

	t.Run("missing markers", func(t *testing.T) {
		root, lock := newFixture(t)
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# ACP\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "missing or has malformed") {
			t.Fatalf("checkLocal err = %v, want missing markers error", err)
		}
	})

	t.Run("duplicate markers", func(t *testing.T) {
		root, lock := newFixture(t)
		raw, err := os.ReadFile(filepath.Join(root, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "README.md"), append(raw, raw...), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("checkLocal err = %v, want duplicate markers error", err)
		}
	})

	t.Run("empty schema digest", func(t *testing.T) {
		root, lock := newFixture(t)
		writeJSON(t, filepath.Join(root, "schema", "v2", "lock.json"), map[string]any{
			"repository": lock.Protocol.Repository,
			"tag":        lock.Protocol.V2Tag,
			"commit":     lock.Protocol.V2Commit,
			"assets":     map[string]string{},
		})
		if err := checkLocal(root, lock); err == nil || !strings.Contains(err.Error(), "assets.schema.json") {
			t.Fatalf("checkLocal err = %v, want empty digest error", err)
		}
	})
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
