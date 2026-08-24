// Command schemaverify verifies vendored ACP schema release assets against
// schema/lock.json.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type schemaLock struct {
	Repository string            `json:"repository"`
	Tag        string            `json:"tag"`
	TagObject  string            `json:"tagObject"`
	Commit     string            `json:"commit"`
	Assets     map[string]string `json:"assets"`
}

func main() {
	schemaDir := flag.String("schema", "schema", "directory containing lock.json and schema assets")
	flag.Parse()
	if err := verify(*schemaDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func verify(schemaDir string) error {
	lockBytes, err := os.ReadFile(filepath.Join(schemaDir, "lock.json"))
	if err != nil {
		return fmt.Errorf("read schema lock: %w", err)
	}
	var lock schemaLock
	if err := json.Unmarshal(lockBytes, &lock); err != nil {
		return fmt.Errorf("parse schema lock: %w", err)
	}
	if lock.Repository == "" || lock.Tag == "" || lock.TagObject == "" || lock.Commit == "" {
		return fmt.Errorf("schema lock is missing immutable source identity")
	}
	if len(lock.Assets) == 0 {
		return fmt.Errorf("schema lock has no assets")
	}

	names := make([]string, 0, len(lock.Assets))
	for name := range lock.Assets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		want := lock.Assets[name]
		if len(want) != sha256.Size*2 {
			return fmt.Errorf("%s: invalid locked SHA256 %q", name, want)
		}
		if _, err := hex.DecodeString(want); err != nil {
			return fmt.Errorf("%s: invalid locked SHA256: %w", name, err)
		}
		file, err := os.Open(filepath.Join(schemaDir, name))
		if err != nil {
			return fmt.Errorf("open %s: %w", name, err)
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hash %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", name, closeErr)
		}
		if got := hex.EncodeToString(hash.Sum(nil)); got != want {
			return fmt.Errorf("%s: SHA256 mismatch: got %s, want %s", name, got, want)
		}
	}
	return nil
}
