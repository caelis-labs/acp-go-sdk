package v2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSchemaLockMatchesVersionConstants(t *testing.T) {
	t.Parallel()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	lockPath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "schema", "v2", "lock.json"))
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(raw, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Tag != SchemaTag {
		t.Fatalf("schema tag = %q, want %q", lock.Tag, SchemaTag)
	}
	if lock.Commit != SchemaCommit {
		t.Fatalf("schema commit = %q, want %q", lock.Commit, SchemaCommit)
	}
	if ProtocolVersionNumber != 2 {
		t.Fatalf("protocol version = %d, want 2", ProtocolVersionNumber)
	}
}
