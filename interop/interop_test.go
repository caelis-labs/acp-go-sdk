package interop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
	"github.com/caelis-labs/acp-go-sdk/transport/stdio"
)

const (
	interopEnabled     = "ACP_INTEROP"
	interopEvidenceDir = "ACP_INTEROP_EVIDENCE_DIR"
)

var (
	repositoryRoot string
	goAgentPath    string
	evidenceMu     sync.Mutex
	evidenceCases  []evidenceCase
)

type peerSpec struct {
	name       string
	agent      func(scenario, resultPath string) (stdio.Command, error)
	clientArgs func(resultPath string) (string, []string, error)
}

type observation struct {
	Peer                    string   `json:"peer"`
	Role                    string   `json:"role"`
	Scenario                string   `json:"scenario"`
	ProtocolVersion         *int     `json:"protocolVersion"`
	CapabilitiesUnsupported *bool    `json:"capabilitiesUnsupported"`
	SessionID               string   `json:"sessionId"`
	StopReason              string   `json:"stopReason"`
	ErrorCode               *int     `json:"errorCode"`
	RequestCancelObserved   *bool    `json:"requestCancelObserved"`
	Events                  []string `json:"events"`
}

type evidenceCase struct {
	Direction   string      `json:"direction"`
	Observation observation `json:"observation"`
}

type evidenceReport struct {
	FormatVersion       int             `json:"formatVersion"`
	GeneratedAt         string          `json:"generatedAt"`
	Status              string          `json:"status"`
	MatrixComplete      bool            `json:"matrixComplete"`
	Failure             string          `json:"failure,omitempty"`
	GoVersion           string          `json:"goVersion"`
	NodeVersion         string          `json:"nodeVersion"`
	RustVersion         string          `json:"rustVersion"`
	GoCommit            string          `json:"goCommit"`
	Dirty               bool            `json:"dirty"`
	WireProtocolVersion int             `json:"wireProtocolVersion"`
	SchemaTag           string          `json:"schemaTag"`
	SchemaCommit        string          `json:"schemaCommit"`
	Versions            json.RawMessage `json:"versions"`
	Cases               []evidenceCase  `json:"cases"`
}

type promptResult struct {
	response acp.PromptResponse
	err      error
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestMain(m *testing.M) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "interop: cannot locate repository root")
		os.Exit(2)
	}
	repositoryRoot = filepath.Clean(filepath.Join(filepath.Dir(currentFile), ".."))

	var temporary string
	if os.Getenv(interopEnabled) == "1" {
		var err error
		temporary, err = os.MkdirTemp("", "acp-go-sdk-interop-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "interop: create temporary directory: %v\n", err)
			os.Exit(2)
		}
		goAgentPath = filepath.Join(temporary, "go-interop-agent")
		command := exec.Command("go", "build", "-o", goAgentPath, "./internal/interoptest/goagent")
		command.Dir = repositoryRoot
		output, err := command.CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "interop: build Go agent: %v\n%s", err, output)
			os.Exit(2)
		}
	}

	code := m.Run()
	if os.Getenv(interopEnabled) == "1" {
		if err := writeEvidenceReport(code); err != nil {
			fmt.Fprintf(os.Stderr, "interop: write evidence: %v\n", err)
			code = 1
		}
	}
	if temporary != "" {
		_ = os.RemoveAll(temporary)
	}
	os.Exit(code)
}

func TestDependencyLocks(t *testing.T) {
	type versionLock struct {
		TypeScript struct {
			Package    string `json:"package"`
			Repository string `json:"repository"`
			Version    string `json:"version"`
			Tag        string `json:"tag"`
			Commit     string `json:"commit"`
			Integrity  string `json:"integrity"`
		} `json:"typescript"`
		Rust struct {
			Package    string `json:"package"`
			Repository string `json:"repository"`
			Version    string `json:"version"`
			Tag        string `json:"tag"`
			Commit     string `json:"commit"`
			Checksum   string `json:"checksum"`
			Toolchain  string `json:"toolchain"`
		} `json:"rust"`
	}

	var locked versionLock
	readJSON(t, filepath.Join(repositoryRoot, "interop", "versions.json"), &locked)
	if locked.TypeScript.Package != "@agentclientprotocol/sdk" ||
		locked.TypeScript.Repository != "https://github.com/agentclientprotocol/typescript-sdk" ||
		locked.TypeScript.Version != "1.4.0" ||
		locked.TypeScript.Tag != "v1.4.0" ||
		locked.TypeScript.Commit != "e6463f444093ed7c5f1cc937c3f32afb5853e906" ||
		locked.TypeScript.Integrity != "sha512-/eufudw+aFY1LKLolT6yFE6UMmYRl7fMJ/DEONSIyR6wI3slHWITBsANRGqXEY8FRzqUxwh7QEaGiZHcJPVThg==" {
		t.Fatalf("unexpected TypeScript lock: %+v", locked.TypeScript)
	}
	if locked.Rust.Package != "agent-client-protocol" ||
		locked.Rust.Repository != "https://github.com/agentclientprotocol/rust-sdk" ||
		locked.Rust.Version != "2.1.0" ||
		locked.Rust.Tag != "v2.1.0" ||
		locked.Rust.Commit != "726c5030bfaa88cfdac2fb1f71a63abb331ce586" ||
		locked.Rust.Checksum != "6395d81d91fd2ee93f48ea31cfc511356e75b9061ca0389cefd0308507cc11ba" ||
		locked.Rust.Toolchain != "1.88.0" {
		t.Fatalf("unexpected Rust lock: %+v", locked.Rust)
	}

	var packageLock struct {
		Packages map[string]struct {
			Version   string `json:"version"`
			Integrity string `json:"integrity"`
		} `json:"packages"`
	}
	readJSON(t, filepath.Join(repositoryRoot, "interop", "peers", "typescript", "package-lock.json"), &packageLock)
	sdk := packageLock.Packages["node_modules/@agentclientprotocol/sdk"]
	if sdk.Version != locked.TypeScript.Version || sdk.Integrity != locked.TypeScript.Integrity {
		t.Fatalf("TypeScript package-lock does not match versions.json: %+v", sdk)
	}

	assertFileContains(t, filepath.Join(repositoryRoot, "interop", "peers", "rust", "Cargo.toml"), fmt.Sprintf(`agent-client-protocol = "=%s"`, locked.Rust.Version))
	assertFileContains(t, filepath.Join(repositoryRoot, "interop", "peers", "rust", "rust-toolchain.toml"), `channel = "1.88.0"`)
	assertFileContains(t, filepath.Join(repositoryRoot, "interop", "peers", "rust", "Cargo.lock"), fmt.Sprintf(`name = %q
version = %q
source = "registry+https://github.com/rust-lang/crates.io-index"
checksum = %q`, locked.Rust.Package, locked.Rust.Version, locked.Rust.Checksum))
}

func TestGoClientAgainstOfficialSDKAgents(t *testing.T) {
	requireInterop(t)
	for _, peer := range peers(t) {
		for _, scenario := range []string{"core", "session-cancel", "request-cancel"} {
			t.Run(peer.name+"/"+scenario, func(t *testing.T) {
				runGoClientScenario(t, peer, scenario)
			})
		}
	}
}

func TestOfficialSDKClientsAgainstGoAgent(t *testing.T) {
	requireInterop(t)
	for _, peer := range peers(t) {
		for _, scenario := range []string{"core", "session-cancel", "request-cancel"} {
			t.Run(peer.name+"/"+scenario, func(t *testing.T) {
				runOfficialClientScenario(t, peer, scenario)
			})
		}
	}
}

func peers(t *testing.T) []peerSpec {
	t.Helper()
	typeScriptPeer := filepath.Join(repositoryRoot, "interop", "peers", "typescript", "dist", "peer.js")
	rustPeer := filepath.Join(repositoryRoot, "interop", "peers", "rust", "target", "debug", "acp-go-sdk-rust-interop-peer")
	return []peerSpec{
		{
			name: "typescript",
			agent: func(scenario, resultPath string) (stdio.Command, error) {
				node, err := exec.LookPath("node")
				return stdio.Command{
					Executable: node,
					Args:       []string{typeScriptPeer, "--role", "agent", "--scenario", scenario, "--result", resultPath},
					Dir:        repositoryRoot,
				}, err
			},
			clientArgs: func(resultPath string) (string, []string, error) {
				node, err := exec.LookPath("node")
				return node, []string{typeScriptPeer, "--role", "client", "--scenario", "SCENARIO", "--agent", goAgentPath, "--result", resultPath}, err
			},
		},
		{
			name: "rust",
			agent: func(scenario, resultPath string) (stdio.Command, error) {
				return stdio.Command{
					Executable: rustPeer,
					Args:       []string{"--role", "agent", "--scenario", scenario, "--result", resultPath},
					Dir:        repositoryRoot,
				}, nil
			},
			clientArgs: func(resultPath string) (string, []string, error) {
				return rustPeer, []string{"--role", "client", "--scenario", "SCENARIO", "--agent", goAgentPath, "--result", resultPath}, nil
			},
		},
	}
}

func runGoClientScenario(t *testing.T, peer peerSpec, scenario string) {
	t.Helper()
	resultPath := filepath.Join(t.TempDir(), "agent-result.json")
	command, err := peer.agent(scenario, resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var stderr lockedBuffer
	command.Stderr = &stderr

	client := newRecordingClient()
	processContext, cancelProcess := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelProcess()
	process, err := stdio.StartClient(processContext, client, command, acp.ConnectionOptions{})
	if err != nil {
		t.Fatalf("start %s agent: %v", peer.name, err)
	}
	defer func() { _ = process.Close() }()

	initialized, err := process.Connection.Initialize(processContext, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersion(acp.WireProtocolVersion),
		ClientInfo: &acp.Implementation{
			Name:    "go-interop-client",
			Version: "0.0.0",
		},
	})
	if err != nil {
		t.Fatalf("initialize %s agent: %v\nstderr:\n%s", peer.name, err, stderr.String())
	}
	if initialized.ProtocolVersion != acp.ProtocolVersion(acp.WireProtocolVersion) {
		t.Fatalf("protocol version = %d, want %d", initialized.ProtocolVersion, acp.WireProtocolVersion)
	}
	if initialized.AgentCapabilities.LoadSession ||
		initialized.AgentCapabilities.PromptCapabilities.Audio ||
		initialized.AgentCapabilities.PromptCapabilities.EmbeddedContext ||
		initialized.AgentCapabilities.PromptCapabilities.Image {
		t.Fatalf("peer advertised capabilities outside the stable core: %+v", initialized.AgentCapabilities)
	}

	session, err := process.Connection.NewSession(processContext, acp.NewSessionRequest{
		Cwd:        repositoryRoot,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		t.Fatalf("new session with %s agent: %v\nstderr:\n%s", peer.name, err, stderr.String())
	}
	request := acp.PromptRequest{
		SessionId: session.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(scenario)},
	}

	switch scenario {
	case "core":
		response, err := process.Connection.Prompt(processContext, request)
		if err != nil {
			t.Fatalf("core prompt to %s agent: %v\nstderr:\n%s", peer.name, err, stderr.String())
		}
		client.append("response:" + string(response.StopReason))

	case "session-cancel":
		result := make(chan promptResult, 1)
		go func() {
			response, promptErr := process.Connection.Prompt(processContext, request)
			result <- promptResult{response: response, err: promptErr}
		}()
		client.waitFor(t, "update:session-cancel-ready")
		if err := process.Connection.Cancel(processContext, acp.CancelNotification{SessionId: session.SessionId}); err != nil {
			t.Fatalf("cancel %s prompt: %v", peer.name, err)
		}
		completed := waitPromptResult(t, result)
		if completed.err != nil {
			t.Fatalf("session cancellation with %s: %v\nstderr:\n%s", peer.name, completed.err, stderr.String())
		}
		client.append("response:" + string(completed.response.StopReason))

	case "request-cancel":
		promptContext, cancelPrompt := context.WithCancel(processContext)
		result := make(chan promptResult, 1)
		go func() {
			response, promptErr := process.Connection.Prompt(promptContext, request)
			result <- promptResult{response: response, err: promptErr}
		}()
		client.waitFor(t, "update:request-cancel-ready")
		cancelPrompt()
		completed := waitPromptResult(t, result)
		var requestError *acp.RequestError
		if !errors.As(completed.err, &requestError) || requestError.Code != -32800 {
			t.Fatalf("request cancellation error = %v, want JSON-RPC -32800", completed.err)
		}
		client.append(fmt.Sprintf("error:%d", requestError.Code))
		marker := waitObservation(t, resultPath)
		if marker.RequestCancelObserved == nil || !*marker.RequestCancelObserved {
			t.Fatalf("%s agent did not record protocol request cancellation: %+v", peer.name, marker)
		}
	}

	events := client.snapshot()
	assertScenarioEvents(t, scenario, events)
	protocolVersion := int(initialized.ProtocolVersion)
	capabilitiesUnsupported := true
	caseObservation := observation{
		Peer:                    peer.name,
		Role:                    "agent",
		Scenario:                scenario,
		ProtocolVersion:         &protocolVersion,
		CapabilitiesUnsupported: &capabilitiesUnsupported,
		SessionID:               string(session.SessionId),
		Events:                  events,
	}
	if scenario == "request-cancel" {
		code := -32800
		observed := true
		caseObservation.ErrorCode = &code
		caseObservation.RequestCancelObserved = &observed
	} else {
		caseObservation.StopReason = map[string]string{
			"core":           "end_turn",
			"session-cancel": "cancelled",
		}[scenario]
	}
	recordEvidence("go-client-to-"+peer.name+"-agent", caseObservation)
	gracefulAgentShutdown(t, process, &stderr)
}

func runOfficialClientScenario(t *testing.T, peer peerSpec, scenario string) {
	t.Helper()
	resultPath := filepath.Join(t.TempDir(), "client-result.json")
	executable, args, err := peer.clientArgs(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	for index, argument := range args {
		if argument == "SCENARIO" {
			args[index] = scenario
		}
	}

	commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, executable, args...)
	command.Dir = repositoryRoot
	var stdout, stderr lockedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("run %s client for %s: %v\nstdout:\n%s\nstderr:\n%s", peer.name, scenario, err, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("%s client wrote unexpected outer stdout:\n%s", peer.name, stdout.String())
	}

	got := waitObservation(t, resultPath)
	if got.Peer != peer.name || got.Role != "client" || got.Scenario != scenario {
		t.Fatalf("unexpected observation identity: %+v", got)
	}
	if got.ProtocolVersion == nil || *got.ProtocolVersion != acp.WireProtocolVersion {
		t.Fatalf("protocol version = %v, want %d", got.ProtocolVersion, acp.WireProtocolVersion)
	}
	if got.CapabilitiesUnsupported == nil || !*got.CapabilitiesUnsupported {
		t.Fatalf("Go agent omitted capabilities were not treated as unsupported: %+v", got)
	}
	if got.SessionID == "" {
		t.Fatal("official client did not record a session id")
	}
	if scenario == "request-cancel" {
		if got.ErrorCode == nil || *got.ErrorCode != -32800 {
			t.Fatalf("request cancellation code = %v, want -32800", got.ErrorCode)
		}
	} else {
		want := map[string]string{"core": "end_turn", "session-cancel": "cancelled"}[scenario]
		if got.StopReason != want {
			t.Fatalf("stop reason = %q, want %q", got.StopReason, want)
		}
	}
	assertScenarioEvents(t, scenario, got.Events)
	recordEvidence(peer.name+"-client-to-go-agent", got)
}

type recordingClient struct {
	mu     sync.Mutex
	events []string
}

func newRecordingClient() *recordingClient {
	return &recordingClient{}
}

func (c *recordingClient) RequestPermission(_ context.Context, request acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	for _, option := range request.Options {
		if option.OptionId == "allow" {
			c.append("permission:allow")
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(option.OptionId),
			}, nil
		}
	}
	return acp.RequestPermissionResponse{}, acp.NewInvalidParams(map[string]any{"error": "allow option missing"})
}

func (c *recordingClient) SessionUpdate(_ context.Context, notification acp.SessionNotification) error {
	if notification.Update.AgentMessageChunk != nil && notification.Update.AgentMessageChunk.Content.Text != nil {
		c.append("update:" + notification.Update.AgentMessageChunk.Content.Text.Text)
	}
	return nil
}

func (c *recordingClient) append(event string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *recordingClient) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.events...)
}

func (c *recordingClient) waitFor(t *testing.T, expected string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range c.snapshot() {
			if event == expected {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; saw %v", expected, c.snapshot())
}

func waitPromptResult(t *testing.T, result <-chan promptResult) promptResult {
	t.Helper()
	select {
	case completed := <-result:
		return completed
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for prompt result")
		return promptResult{}
	}
}

func gracefulAgentShutdown(t *testing.T, process *stdio.ClientProcess, stderr *lockedBuffer) {
	t.Helper()
	if err := process.Process.Input().Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Fatalf("close peer stdin: %v", err)
	}
	waitContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Wait(waitContext); err != nil {
		t.Fatalf("peer did not exit cleanly: %v\nstderr:\n%s", err, stderr.String())
	}
}

func waitObservation(t *testing.T, path string) observation {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var got observation
		if err := decodeJSON(path, &got); err == nil {
			return got
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("decode observation %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for observation %s", path)
	return observation{}
}

func assertScenarioEvents(t *testing.T, scenario string, got []string) {
	t.Helper()
	if scenario == "core" {
		// Notifications are ordered relative to one another, while inbound
		// reverse requests may run concurrently with their handlers. Some
		// official SDK schedulers therefore deliver the permission request
		// before the two already-written updates have finished handling.
		positions := make(map[string]int, len(got))
		for index, event := range got {
			if _, duplicate := positions[event]; duplicate {
				t.Fatalf("duplicate core event %q in %v", event, got)
			}
			positions[event] = index
		}
		wantEvents := []string{"update:core-1", "update:core-2", "permission:allow", "update:core-3", "response:end_turn"}
		if len(got) != len(wantEvents) {
			t.Fatalf("core events = %v, want exactly %v", got, wantEvents)
		}
		for _, event := range wantEvents {
			if _, ok := positions[event]; !ok {
				t.Fatalf("core events = %v, missing %q", got, event)
			}
		}
		ordered := positions["update:core-1"] < positions["update:core-2"] &&
			positions["update:core-2"] < positions["update:core-3"] &&
			positions["permission:allow"] < positions["update:core-3"] &&
			positions["update:core-3"] < positions["response:end_turn"]
		if !ordered {
			t.Fatalf("core event ordering = %v", got)
		}
		return
	}
	want := map[string][]string{
		"session-cancel": {"update:session-cancel-ready", "response:cancelled"},
		"request-cancel": {"update:request-cancel-ready", "error:-32800"},
	}[scenario]
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func requireInterop(t *testing.T) {
	t.Helper()
	if os.Getenv(interopEnabled) != "1" {
		t.Skip("set ACP_INTEROP=1 or run make interop")
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	if err := decodeJSON(path, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func decodeJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func assertFileContains(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(expected)) {
		t.Fatalf("%s does not contain %q", path, expected)
	}
}

func recordEvidence(direction string, got observation) {
	evidenceMu.Lock()
	defer evidenceMu.Unlock()
	evidenceCases = append(evidenceCases, evidenceCase{
		Direction:   direction,
		Observation: got,
	})
}

func writeEvidenceReport(testCode int) error {
	directory := os.Getenv(interopEvidenceDir)
	if directory == "" {
		return nil
	}

	versions, err := os.ReadFile(filepath.Join(repositoryRoot, "interop", "versions.json"))
	if err != nil {
		return fmt.Errorf("read versions lock: %w", err)
	}
	if !json.Valid(versions) {
		return errors.New("interop/versions.json is not valid JSON")
	}

	evidenceMu.Lock()
	cases := append([]evidenceCase(nil), evidenceCases...)
	evidenceMu.Unlock()
	sort.Slice(cases, func(i, j int) bool {
		if cases[i].Direction != cases[j].Direction {
			return cases[i].Direction < cases[j].Direction
		}
		return cases[i].Observation.Scenario < cases[j].Observation.Scenario
	})

	matrixErr := validateEvidenceCases(cases)
	status := "passed"
	if testCode != 0 || matrixErr != nil {
		status = "failed"
	}
	report := evidenceReport{
		FormatVersion:       1,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		Status:              status,
		MatrixComplete:      matrixErr == nil,
		GoVersion:           runtime.Version(),
		NodeVersion:         commandOutput(repositoryRoot, "node", "--version"),
		RustVersion:         commandOutput(filepath.Join(repositoryRoot, "interop", "peers", "rust"), "rustc", "--version"),
		GoCommit:            commandOutput(repositoryRoot, "git", "rev-parse", "HEAD"),
		Dirty:               commandOutput(repositoryRoot, "git", "status", "--porcelain") != "",
		WireProtocolVersion: acp.WireProtocolVersion,
		SchemaTag:           acp.SchemaTag,
		SchemaCommit:        acp.SchemaCommit,
		Versions:            json.RawMessage(versions),
		Cases:               cases,
	}
	if matrixErr != nil {
		report.Failure = matrixErr.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode evidence report: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create evidence directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "evidence.json"), data, 0o644); err != nil {
		return fmt.Errorf("write evidence report: %w", err)
	}
	if matrixErr != nil {
		return matrixErr
	}
	return nil
}

func validateEvidenceCases(cases []evidenceCase) error {
	directions := []string{
		"go-client-to-rust-agent",
		"go-client-to-typescript-agent",
		"rust-client-to-go-agent",
		"typescript-client-to-go-agent",
	}
	scenarios := []string{"core", "request-cancel", "session-cancel"}
	expected := make(map[string]struct{}, len(directions)*len(scenarios))
	for _, direction := range directions {
		for _, scenario := range scenarios {
			expected[direction+"/"+scenario] = struct{}{}
		}
	}
	for _, evidence := range cases {
		key := evidence.Direction + "/" + evidence.Observation.Scenario
		if _, ok := expected[key]; !ok {
			return fmt.Errorf("unexpected or duplicate evidence case %q", key)
		}
		delete(expected, key)
	}
	if len(expected) == 0 {
		return nil
	}
	missing := make([]string, 0, len(expected))
	for key := range expected {
		missing = append(missing, key)
	}
	sort.Strings(missing)
	return fmt.Errorf("missing evidence cases: %s", strings.Join(missing, ", "))
}

func commandOutput(directory, executable string, args ...string) string {
	command := exec.Command(executable, args...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	return strings.TrimSpace(string(output))
}

var _ acp.Client = (*recordingClient)(nil)
