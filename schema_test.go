package acp

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSchemaLockMatchesPublicVersionConstants(t *testing.T) {
	lockBytes, err := os.ReadFile("schema/lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	}
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		t.Fatal(err)
	}
	if lock.Tag != SchemaTag {
		t.Fatalf("schema tag = %q, want %q", lock.Tag, SchemaTag)
	}
	if lock.Commit != SchemaCommit {
		t.Fatalf("schema commit = %q, want %q", lock.Commit, SchemaCommit)
	}
	version, err := os.ReadFile("schema/version")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(version); got != SchemaArtifactVersion+"\n" {
		t.Fatalf("schema version = %q, want %q", got, SchemaArtifactVersion)
	}
}
