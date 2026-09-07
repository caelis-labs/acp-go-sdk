package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultGitHubAPI = "https://api.github.com"
	githubUserAgent  = "caelis-labs-acp-go-sdk-upstreamdrift"
	maxGitHubBody    = 1 << 20
)

type upstreamLock struct {
	Protocol      protocolLock `json:"protocol"`
	TypeScriptSDK sdkLock      `json:"typescriptSdk"`
	RustSDK       sdkLock      `json:"rustSdk"`
}

type protocolLock struct {
	Repository   string `json:"repository"`
	StableTag    string `json:"stableTag"`
	StableCommit string `json:"stableCommit"`
	V2Tag        string `json:"v2Tag"`
	V2Commit     string `json:"v2Commit"`
}

type sdkLock struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

type schemaLockFile struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

type interopLockFile struct {
	TypeScript struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Commit     string `json:"commit"`
	} `json:"typescript"`
	Rust struct {
		Repository string `json:"repository"`
		Tag        string `json:"tag"`
		Commit     string `json:"commit"`
	} `json:"rust"`
}

type githubRef struct {
	Ref string `json:"ref"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

type finding struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Pinned string `json:"pinned"`
	Latest string `json:"latest"`
	URL    string `json:"url"`
}

type report struct {
	Status     string    `json:"status"`
	IssueTitle string    `json:"issueTitle,omitempty"`
	IssueBody  string    `json:"issueBody,omitempty"`
	Findings   []finding `json:"findings"`
}

type githubClient struct {
	http    *http.Client
	baseURL string
	token   string
}

func loadLock(path string) (upstreamLock, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return upstreamLock{}, fmt.Errorf("read upstream lock: %w", err)
	}
	var lock upstreamLock
	if err := json.Unmarshal(raw, &lock); err != nil {
		return upstreamLock{}, fmt.Errorf("parse upstream lock: %w", err)
	}
	if err := validateLock(lock); err != nil {
		return upstreamLock{}, err
	}
	return lock, nil
}

func validateLock(lock upstreamLock) error {
	if err := requireGitHubRepo("protocol.repository", lock.Protocol.Repository); err != nil {
		return err
	}
	if err := requireTag("protocol.stableTag", lock.Protocol.StableTag); err != nil {
		return err
	}
	if err := requireCommit("protocol.stableCommit", lock.Protocol.StableCommit); err != nil {
		return err
	}
	if err := requireTag("protocol.v2Tag", lock.Protocol.V2Tag); err != nil {
		return err
	}
	if err := requireCommit("protocol.v2Commit", lock.Protocol.V2Commit); err != nil {
		return err
	}
	if err := validateSDKLock("typescriptSdk", lock.TypeScriptSDK); err != nil {
		return err
	}
	if err := validateSDKLock("rustSdk", lock.RustSDK); err != nil {
		return err
	}
	return nil
}

func validateSDKLock(field string, lock sdkLock) error {
	if err := requireGitHubRepo(field+".repository", lock.Repository); err != nil {
		return err
	}
	if err := requireTag(field+".tag", lock.Tag); err != nil {
		return err
	}
	return requireCommit(field+".commit", lock.Commit)
}

func requireGitHubRepo(field, raw string) error {
	if _, _, err := parseGitHubRepo(raw); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func requireTag(field, tag string) error {
	if _, err := parseTag(tag); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func requireCommit(field, commit string) error {
	if len(commit) != 40 {
		return fmt.Errorf("%s: commit must be a 40-character lowercase SHA", field)
	}
	for _, r := range commit {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return fmt.Errorf("%s: commit must be a 40-character lowercase SHA", field)
		}
	}
	return nil
}

func parseGitHubRepo(raw string) (owner, repo string, err error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("invalid repository URL %q", raw)
	}
	if parsed.Scheme != "https" || parsed.Host != "github.com" {
		return "", "", fmt.Errorf("repository URL must be https://github.com/{owner}/{repo}")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repository URL must be https://github.com/{owner}/{repo}")
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func checkLocal(repoRoot string, lock upstreamLock) error {
	var schema schemaLockFile
	if err := readJSON(filepath.Join(repoRoot, "schema", "lock.json"), &schema); err != nil {
		return fmt.Errorf("read schema lock: %w", err)
	}
	if schema.Repository != lock.Protocol.Repository ||
		schema.Tag != lock.Protocol.StableTag ||
		schema.Commit != lock.Protocol.StableCommit {
		return fmt.Errorf("upstream protocol stable pin does not match schema/lock.json")
	}

	var interop interopLockFile
	if err := readJSON(filepath.Join(repoRoot, "interop", "versions.json"), &interop); err != nil {
		return fmt.Errorf("read interop versions: %w", err)
	}
	if interop.TypeScript.Repository != lock.TypeScriptSDK.Repository ||
		interop.TypeScript.Tag != lock.TypeScriptSDK.Tag ||
		interop.TypeScript.Commit != lock.TypeScriptSDK.Commit {
		return fmt.Errorf("upstream TypeScript pin does not match interop/versions.json")
	}
	if interop.Rust.Repository != lock.RustSDK.Repository ||
		interop.Rust.Tag != lock.RustSDK.Tag ||
		interop.Rust.Commit != lock.RustSDK.Commit {
		return fmt.Errorf("upstream Rust pin does not match interop/versions.json")
	}
	return nil
}

func checkRemote(ctx context.Context, gh *githubClient, lock upstreamLock) ([]finding, error) {
	var findings []finding

	stable, err := latestSchemaTag(ctx, gh, lock.Protocol.Repository, "schema-v1", func(v version) bool {
		return strings.HasPrefix(v.raw, "schema-v1.") && len(v.pre) == 0
	})
	if err != nil {
		return nil, fmt.Errorf("stable schema: %w", err)
	}
	findings = appendFinding(findings, finding{
		Source: "protocolStable",
		Name:   "stable-schema",
		Pinned: lock.Protocol.StableTag,
		Latest: stable,
		URL:    releaseURL(lock.Protocol.Repository, stable),
	})

	v2, err := latestSchemaTag(ctx, gh, lock.Protocol.Repository, "schema-v2", func(v version) bool {
		return strings.HasPrefix(v.raw, "schema-v2.")
	})
	if err != nil {
		return nil, fmt.Errorf("v2 schema: %w", err)
	}
	findings = appendFinding(findings, finding{
		Source: "protocolV2",
		Name:   "v2-schema",
		Pinned: lock.Protocol.V2Tag,
		Latest: v2,
		URL:    releaseURL(lock.Protocol.Repository, v2),
	})

	typescript, err := latestReleaseTag(ctx, gh, lock.TypeScriptSDK.Repository)
	if err != nil {
		return nil, fmt.Errorf("typescript SDK: %w", err)
	}
	findings = appendFinding(findings, finding{
		Source: "typescriptSdk",
		Name:   "typescript-sdk",
		Pinned: lock.TypeScriptSDK.Tag,
		Latest: typescript,
		URL:    releaseURL(lock.TypeScriptSDK.Repository, typescript),
	})

	rust, err := latestReleaseTag(ctx, gh, lock.RustSDK.Repository)
	if err != nil {
		return nil, fmt.Errorf("rust SDK: %w", err)
	}
	findings = appendFinding(findings, finding{
		Source: "rustSdk",
		Name:   "rust-sdk",
		Pinned: lock.RustSDK.Tag,
		Latest: rust,
		URL:    releaseURL(lock.RustSDK.Repository, rust),
	})

	return findings, nil
}

func appendFinding(findings []finding, item finding) []finding {
	pinned, err := parseTag(item.Pinned)
	if err != nil {
		return append(findings, item)
	}
	latest, err := parseTag(item.Latest)
	if err != nil {
		return append(findings, item)
	}
	if latest.Compare(pinned) > 0 {
		return append(findings, item)
	}
	return findings
}

func latestSchemaTag(ctx context.Context, gh *githubClient, repository, prefix string, include func(version) bool) (string, error) {
	owner, repo, err := parseGitHubRepo(repository)
	if err != nil {
		return "", err
	}
	var refs []githubRef
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/git/matching-refs/tags/" + prefix
	if err := gh.getJSON(ctx, path, &refs); err != nil {
		return "", err
	}
	tags := make([]string, 0, len(refs))
	for _, ref := range refs {
		tags = append(tags, strings.TrimPrefix(ref.Ref, "refs/tags/"))
	}
	return latestTag(tags, include)
}

func latestReleaseTag(ctx context.Context, gh *githubClient, repository string) (string, error) {
	owner, repo, err := parseGitHubRepo(repository)
	if err != nil {
		return "", err
	}
	var release githubRelease
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/releases/latest"
	if err := gh.getJSON(ctx, path, &release); err != nil {
		return "", err
	}
	if _, err := parseTag(release.TagName); err != nil {
		return "", fmt.Errorf("latest release tag %q: %w", release.TagName, err)
	}
	return release.TagName, nil
}

func (c *githubClient) getJSON(ctx context.Context, path string, dest any) error {
	endpoint := strings.TrimRight(c.baseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", githubUserAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxGitHubBody+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read %s: %w", path, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if len(body) > maxGitHubBody {
		return fmt.Errorf("%s: response exceeds %d bytes", path, maxGitHubBody)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, dest any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}

func releaseURL(repository, tag string) string {
	return strings.TrimRight(repository, "/") + "/releases/tag/" + tag
}

func buildReport(findings []finding) report {
	if len(findings) == 0 {
		return report{Status: "current", Findings: []finding{}}
	}
	return report{
		Status:     "drift",
		IssueTitle: issueTitle(findings),
		IssueBody:  issueBody(findings),
		Findings:   findings,
	}
}

func issueTitle(findings []finding) string {
	if len(findings) == 1 {
		return fmt.Sprintf("upstream-drift: %s %s available; pinned %s", findings[0].Name, findings[0].Latest, findings[0].Pinned)
	}
	parts := make([]string, 0, len(findings))
	for _, item := range findings {
		parts = append(parts, item.Name+" "+item.Latest)
	}
	return "upstream-drift: " + strings.Join(parts, ", ") + " available"
}

func issueBody(findings []finding) string {
	var b strings.Builder
	b.WriteString("Automated upstream drift check found newer official releases than the pins in `upstream/lock.json`.\n\n")
	b.WriteString("| Source | Pinned | Latest |\n| --- | --- | --- |\n")
	for _, item := range findings {
		fmt.Fprintf(&b, "| %s | `%s` | [`%s`](%s) |\n", item.Name, item.Pinned, item.Latest, item.URL)
	}
	b.WriteString("\nThis is a tracking issue, not proof of a wire incompatibility.\n\n")
	b.WriteString("- Stable schema: lock, regenerate, review, and rerun interop within one week.\n")
	b.WriteString("- Official SDKs: bump `interop/versions.json` and rerun the four-direction matrix even when the wire schema is unchanged.\n")
	b.WriteString("- Draft v2: snapshot tagged `schema-v2*` releases only; do not merge v2 into the stable root package.\n")
	return b.String()
}
