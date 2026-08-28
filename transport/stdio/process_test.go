package stdio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("ACP_STDIO_GRANDCHILD_HELPER") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("ACP_STDIO_TREE_HELPER") == "1" {
		grandchild := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$")
		grandchild.Env = append(os.Environ(), "ACP_STDIO_GRANDCHILD_HELPER=1")
		if err := grandchild.Start(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "start grandchild: %v", err)
			os.Exit(2)
		}
		pidFile := os.Getenv("ACP_STDIO_PID_FILE")
		pids := fmt.Sprintf("%d\n%d\n", os.Getpid(), grandchild.Process.Pid)
		if err := os.WriteFile(pidFile, []byte(pids), 0o600); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "write pid file: %v", err)
			os.Exit(2)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("ACP_STDIO_IGNORE_EOF_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		for {
			time.Sleep(time.Hour)
		}
	}
	if os.Getenv("ACP_STDIO_BLOCK_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	if os.Getenv("ACP_STDIO_HELPER") != "1" {
		return
	}
	_, _ = io.WriteString(os.Stderr, strings.Repeat("stderr must be drained\n", 2048))
	_, _ = io.WriteString(os.Stdout, "{\"jsonrpc\":\"2.0\",\"method\":\"ready\"}\n")
	os.Exit(0)
}

func TestProcessShutdownAllowsGracefulEOFExit(t *testing.T) {
	process, err := Start(context.Background(), Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_BLOCK_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := process.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown = %v, want graceful success", err)
	}
	if err := process.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeat Shutdown = %v, want cached graceful success", err)
	}
	if err := process.Wait(shutdownCtx); err != nil {
		t.Fatalf("Wait after Shutdown = %v", err)
	}
	if !process.waitComplete() {
		t.Fatal("Shutdown returned before the internal waiter completed")
	}
}

func TestClientProcessShutdownJoinsConnectionAndProcess(t *testing.T) {
	process, err := StartClient(context.Background(), noopClient{}, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_BLOCK_HELPER=1"),
	}, acp.ConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := process.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown = %v, want graceful success", err)
	}
	repeatCtx, cancelRepeat := context.WithCancel(context.Background())
	cancelRepeat()
	if err := process.Shutdown(repeatCtx); err != nil {
		t.Fatalf("repeat ClientProcess.Shutdown = %v, want cached graceful success", err)
	}
	select {
	case <-process.Connection.Done():
	default:
		t.Fatal("ClientProcess.Shutdown returned before connection termination")
	}
	if !process.Process.waitComplete() {
		t.Fatal("ClientProcess.Shutdown returned before process waiter completion")
	}
}

func TestProcessShutdownTerminatesOwnedProcessTree(t *testing.T) {
	pidFile := t.TempDir() + string(os.PathSeparator) + "pids"
	process, err := Start(context.Background(), Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env: append(os.Environ(),
			"ACP_STDIO_TREE_HELPER=1",
			"ACP_STDIO_PID_FILE="+pidFile,
		),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	pids := waitForHelperPIDs(t, pidFile)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	shutdownErr := process.Shutdown(shutdownCtx)
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want deadline exceeded after forced cleanup", shutdownErr)
	}
	repeatErr := process.Shutdown(context.Background())
	if !errors.Is(repeatErr, context.DeadlineExceeded) || repeatErr.Error() != shutdownErr.Error() {
		t.Fatalf("repeat Shutdown = %v, want cached %v", repeatErr, shutdownErr)
	}
	if !process.waitComplete() {
		t.Fatal("forced Shutdown returned before the internal waiter completed")
	}
	for _, pid := range pids {
		assertProcessExited(t, pid)
	}
}

func TestStartContextCancellationTerminatesOwnedProcessTree(t *testing.T) {
	pidFile := t.TempDir() + string(os.PathSeparator) + "pids"
	startCtx, cancelStart := context.WithCancel(context.Background())
	process, err := Start(startCtx, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env: append(os.Environ(),
			"ACP_STDIO_TREE_HELPER=1",
			"ACP_STDIO_PID_FILE="+pidFile,
		),
	})
	if err != nil {
		cancelStart()
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()
	pids := waitForHelperPIDs(t, pidFile)
	cancelStart()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	if err := process.Wait(waitCtx); err == nil {
		t.Fatal("Wait after start-context cancellation succeeded, want forced exit error")
	}
	for _, pid := range pids {
		assertProcessExited(t, pid)
	}
}

func TestConcurrentProcessCloseShutdownAndWait(t *testing.T) {
	process, err := Start(context.Background(), Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_IGNORE_EOF_HELPER=1"),
	})
	if err != nil {
		t.Fatal(err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelShutdown()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		_ = process.Shutdown(shutdownCtx)
	}()
	go func() {
		defer wg.Done()
		_ = process.Close()
	}()
	go func() {
		defer wg.Done()
		_ = process.Wait(waitCtx)
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Close, Shutdown, and Wait deadlocked")
	}
	if !process.waitComplete() {
		t.Fatal("concurrent shutdown left the internal waiter running")
	}
	if err := process.Close(); err != nil {
		t.Fatalf("repeat Close = %v", err)
	}
}

func waitForHelperPIDs(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Fields(string(contents))
			if len(lines) == 2 {
				pids := make([]int, 0, len(lines))
				for _, line := range lines {
					pid, err := strconv.Atoi(line)
					if err != nil {
						t.Fatal(err)
					}
					pids = append(pids, pid)
				}
				return pids
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper pid file %q was not ready", path)
	return nil
}

type noopClient struct{}

func (noopClient) RequestPermission(context.Context, acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	return acp.RequestPermissionResponse{}, nil
}

func (noopClient) SessionUpdate(context.Context, acp.SessionNotification) error { return nil }

func TestClientProcessWaitTimeoutDoesNotCloseConnection(t *testing.T) {
	process, err := StartClient(context.Background(), noopClient{}, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_BLOCK_HELPER=1"),
	}, acp.ConnectionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	waitCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := process.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want deadline exceeded", err)
	}
	select {
	case <-process.Connection.Done():
		t.Fatal("Wait timeout closed the active protocol connection")
	default:
	}
}

var _ acp.Client = noopClient{}

func TestStartDrainsStderrAndWaitIsRepeatable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	process, err := Start(ctx, Command{
		Executable: os.Args[0],
		Args:       []string{"-test.run=^TestProcessHelper$"},
		Env:        append(os.Environ(), "ACP_STDIO_HELPER=1"),
		Stderr:     &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Close() }()

	protocol, err := io.ReadAll(process.Output())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(ctx); err != nil {
		t.Fatalf("second Wait = %v", err)
	}
	if got := string(protocol); got != "{\"jsonrpc\":\"2.0\",\"method\":\"ready\"}\n" {
		t.Fatalf("protocol stdout = %q", got)
	}
	if got := strings.Count(stderr.String(), "stderr must be drained"); got != 2048 {
		t.Fatalf("drained stderr lines = %d, want 2048", got)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
	if err := process.Close(); err != nil {
		t.Fatal(err)
	}
}
