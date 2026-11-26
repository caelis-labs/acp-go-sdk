package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type docTarget struct {
	relPath     string
	startMarker string
	endMarker   string
	render      func(upstreamLock, schemaDetails, schemaDetails, interopDetails) string
}

type schemaDetails struct {
	Repository string            `json:"repository"`
	Tag        string            `json:"tag"`
	Commit     string            `json:"commit"`
	Assets     map[string]string `json:"assets"`
}

type interopDetails struct {
	TypeScript struct {
		Package    string `json:"package"`
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Commit     string `json:"commit"`
	} `json:"typescript"`
	Rust struct {
		Package    string `json:"package"`
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Commit     string `json:"commit"`
		Toolchain  string `json:"toolchain"`
	} `json:"rust"`
}

var managedDocTargets = []docTarget{
	{
		relPath:     "README.md",
		startMarker: "<!-- upstream-pins:en:start -->",
		endMarker:   "<!-- upstream-pins:en:end -->",
		render:      renderEnglishReadmePins,
	},
	{
		relPath:     "README.zh-CN.md",
		startMarker: "<!-- upstream-pins:zh-CN:start -->",
		endMarker:   "<!-- upstream-pins:zh-CN:end -->",
		render:      renderChineseReadmePins,
	},
	{
		relPath:     filepath.Join("docs", "experimental-v2.md"),
		startMarker: "<!-- upstream-pins:v2:start -->",
		endMarker:   "<!-- upstream-pins:v2:end -->",
		render:      renderExperimentalV2Pins,
	},
}

func loadPinDetails(repoRoot string) (schemaDetails, schemaDetails, interopDetails, error) {
	var v1 schemaDetails
	if err := readJSON(filepath.Join(repoRoot, "schema", "lock.json"), &v1); err != nil {
		return schemaDetails{}, schemaDetails{}, interopDetails{}, fmt.Errorf("read schema/lock.json: %w", err)
	}
	var v2 schemaDetails
	v2Path := filepath.Join(repoRoot, "schema", "v2", "lock.json")
	if _, err := os.Stat(v2Path); err == nil {
		if err := readJSON(v2Path, &v2); err != nil {
			return schemaDetails{}, schemaDetails{}, interopDetails{}, fmt.Errorf("read schema/v2/lock.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return schemaDetails{}, schemaDetails{}, interopDetails{}, fmt.Errorf("stat schema/v2/lock.json: %w", err)
	}
	var interop interopDetails
	if err := readJSON(filepath.Join(repoRoot, "interop", "versions.json"), &interop); err != nil {
		return schemaDetails{}, schemaDetails{}, interopDetails{}, fmt.Errorf("read interop/versions.json: %w", err)
	}
	return v1, v2, interop, nil
}

func checkDocs(repoRoot string, lock upstreamLock) error {
	v1, v2, interop, err := loadPinDetails(repoRoot)
	if err != nil {
		return err
	}
	for _, target := range managedDocTargets {
		path := filepath.Join(repoRoot, target.relPath)
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", target.relPath, err)
		}
		content := string(raw)
		startIdx := strings.Index(content, target.startMarker)
		endIdx := strings.Index(content, target.endMarker)
		if startIdx < 0 && endIdx < 0 {
			continue
		}
		if startIdx < 0 || endIdx < 0 || endIdx < startIdx {
			return fmt.Errorf("%s has malformed upstream pin markers (%s ... %s)", target.relPath, target.startMarker, target.endMarker)
		}
		actual := content[startIdx : endIdx+len(target.endMarker)]
		want := wrapMarkerBlock(target.startMarker, target.endMarker, target.render(lock, v1, v2, interop))
		if actual != want {
			return fmt.Errorf("%s upstream pin block is out of date; run 'go run ./cmd/upstreamdrift -sync-docs'", target.relPath)
		}
	}
	return nil
}

func syncDocs(repoRoot string, lock upstreamLock) ([]string, error) {
	v1, v2, interop, err := loadPinDetails(repoRoot)
	if err != nil {
		return nil, err
	}
	var updated []string
	for _, target := range managedDocTargets {
		path := filepath.Join(repoRoot, target.relPath)
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", target.relPath, err)
		}
		content := string(raw)
		startIdx := strings.Index(content, target.startMarker)
		endIdx := strings.Index(content, target.endMarker)
		if startIdx < 0 && endIdx < 0 {
			continue
		}
		if startIdx < 0 || endIdx < 0 || endIdx < startIdx {
			return nil, fmt.Errorf("%s has malformed upstream pin markers (%s ... %s)", target.relPath, target.startMarker, target.endMarker)
		}
		want := wrapMarkerBlock(target.startMarker, target.endMarker, target.render(lock, v1, v2, interop))
		next := content[:startIdx] + want + content[endIdx+len(target.endMarker):]
		if next == content {
			continue
		}
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", target.relPath, err)
		}
		updated = append(updated, target.relPath)
	}
	return updated, nil
}

func wrapMarkerBlock(startMarker, endMarker, body string) string {
	return startMarker + "\n" + strings.TrimSpace(body) + "\n" + endMarker
}

func shortDigest(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func repoLink(repoURL string) string {
	trimmed := strings.TrimPrefix(strings.TrimRight(repoURL, "/"), "https://github.com/")
	return fmt.Sprintf("[`%s`](%s)", trimmed, repoURL)
}

func renderEnglishReadmePins(lock upstreamLock, v1 schemaDetails, v2 schemaDetails, interop interopDetails) string {
	v1Row := fmt.Sprintf(
		"| **Stable ACP v1 Schema** (wire `1`) | %s | `%s` | `%s` | [`schema/lock.json`](schema/lock.json) (`schema.json` SHA256 `%s...`) |",
		repoLink(lock.Protocol.Repository),
		lock.Protocol.StableTag,
		shortDigest(lock.Protocol.StableCommit),
		shortDigest(v1.Assets["schema.json"]),
	)
	v2Row := fmt.Sprintf(
		"| **Draft ACP v2 Schema** (`experimental/v2`) | %s | `%s` | `%s` | [`schema/v2/lock.json`](schema/v2/lock.json) (`schema.json` SHA256 `%s...`) |",
		repoLink(lock.Protocol.Repository),
		lock.Protocol.V2Tag,
		shortDigest(lock.Protocol.V2Commit),
		shortDigest(v2.Assets["schema.json"]),
	)
	tsRow := fmt.Sprintf(
		"| **Official TypeScript SDK** | %s | `%s` (`%s`) | `%s` | [`interop/versions.json`](interop/versions.json) |",
		repoLink(lock.TypeScriptSDK.Repository),
		lock.TypeScriptSDK.Tag,
		interop.TypeScript.Package,
		shortDigest(lock.TypeScriptSDK.Commit),
	)
	rustRow := fmt.Sprintf(
		"| **Official Rust SDK** | %s | `%s` (`%s`, Rust `%s`) | `%s` | [`interop/versions.json`](interop/versions.json) |",
		repoLink(lock.RustSDK.Repository),
		lock.RustSDK.Tag,
		interop.Rust.Package,
		interop.Rust.Toolchain,
		shortDigest(lock.RustSDK.Commit),
	)
	return strings.Join([]string{
		"| Tracked upstream artifact | Repository | Pinned tag / version | Upstream commit | Verification / lock file |",
		"|---|---|---|---|---|",
		v1Row,
		v2Row,
		tsRow,
		rustRow,
	}, "\n")
}

func renderChineseReadmePins(lock upstreamLock, v1 schemaDetails, v2 schemaDetails, interop interopDetails) string {
	v1Row := fmt.Sprintf(
		"| **ACP v1 稳定 Schema**（线协议 `1`） | %s | `%s` | `%s` | [`schema/lock.json`](schema/lock.json)（`schema.json` SHA256 `%s...`） |",
		repoLink(lock.Protocol.Repository),
		lock.Protocol.StableTag,
		shortDigest(lock.Protocol.StableCommit),
		shortDigest(v1.Assets["schema.json"]),
	)
	v2Row := fmt.Sprintf(
		"| **ACP v2 草案 Schema**（`experimental/v2`） | %s | `%s` | `%s` | [`schema/v2/lock.json`](schema/v2/lock.json)（`schema.json` SHA256 `%s...`） |",
		repoLink(lock.Protocol.Repository),
		lock.Protocol.V2Tag,
		shortDigest(lock.Protocol.V2Commit),
		shortDigest(v2.Assets["schema.json"]),
	)
	tsRow := fmt.Sprintf(
		"| **官方 TypeScript SDK** | %s | `%s`（`%s`） | `%s` | [`interop/versions.json`](interop/versions.json) |",
		repoLink(lock.TypeScriptSDK.Repository),
		lock.TypeScriptSDK.Tag,
		interop.TypeScript.Package,
		shortDigest(lock.TypeScriptSDK.Commit),
	)
	rustRow := fmt.Sprintf(
		"| **官方 Rust SDK** | %s | `%s`（`%s`，Rust `%s`） | `%s` | [`interop/versions.json`](interop/versions.json) |",
		repoLink(lock.RustSDK.Repository),
		lock.RustSDK.Tag,
		interop.Rust.Package,
		interop.Rust.Toolchain,
		shortDigest(lock.RustSDK.Commit),
	)
	return strings.Join([]string{
		"| 追踪的上游组件 | 上游仓库 | 当前固定版本 (Pin) | 上游 Commit | 校验 / 锁定文件 |",
		"|---|---|---|---|---|",
		v1Row,
		v2Row,
		tsRow,
		rustRow,
	}, "\n")
}

func renderExperimentalV2Pins(lock upstreamLock, _ schemaDetails, v2 schemaDetails, _ interopDetails) string {
	return strings.Join([]string{
		"| Identity | Pinned value |",
		"|---|---|",
		fmt.Sprintf("| Upstream repository | %s |", repoLink(lock.Protocol.Repository)),
		fmt.Sprintf("| Schema tag | `%s` |", lock.Protocol.V2Tag),
		fmt.Sprintf("| Upstream commit | `%s` |", lock.Protocol.V2Commit),
		fmt.Sprintf("| `schema/v2/schema.json` SHA256 | `%s` |", v2.Assets["schema.json"]),
	}, "\n")
}
