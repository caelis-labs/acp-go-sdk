//go:build windows

package stdio

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsShutdownConvergesWhileDescendantsSpawn(t *testing.T) {
	for iteration := 0; iteration < 5; iteration++ {
		process, err := Start(context.Background(), Command{
			Executable: os.Args[0],
			Args:       []string{"-test.run=^TestProcessHelper$"},
			Env:        append(os.Environ(), "ACP_STDIO_SPAWN_STRESS_HELPER=1"),
		})
		if err != nil {
			t.Fatal(err)
		}
		job, ok := process.tree.(*windowsJob)
		if !ok {
			_ = process.Close()
			t.Fatalf("process tree = %T, want *windowsJob", process.tree)
		}
		jobHandle := duplicateWindowsHandle(t, job.handle)
		waitForJobMembers(t, jobHandle, 4)

		shutdownCtx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := process.Shutdown(shutdownCtx); !errors.Is(err, context.Canceled) {
			_ = windows.CloseHandle(jobHandle)
			t.Fatalf("Shutdown = %v, want context canceled after forced cleanup", err)
		}
		processIDs, err := queryJobProcessIDs(jobHandle)
		closeErr := windows.CloseHandle(jobHandle)
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if len(processIDs) != 0 {
			t.Fatalf("Shutdown returned with active Job Object process IDs: %v", processIDs)
		}
	}
}

func duplicateWindowsHandle(t *testing.T, source windows.Handle) windows.Handle {
	t.Helper()
	currentProcess := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(
		currentProcess,
		source,
		currentProcess,
		&duplicate,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	); err != nil {
		t.Fatal(err)
	}
	return duplicate
}

func waitForJobMembers(t *testing.T, job windows.Handle, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		processIDs, err := queryJobProcessIDs(job)
		if err != nil {
			t.Fatal(err)
		}
		if len(processIDs) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Job Object did not reach %d concurrent members", want)
}

func captureProcessExitAssertions(t *testing.T, pids []int) func() {
	t.Helper()
	type processAssertion struct {
		pid    int
		handle windows.Handle
	}
	assertions := make([]processAssertion, 0, len(pids))
	for _, pid := range pids {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue
			}
			for _, assertion := range assertions {
				_ = windows.CloseHandle(assertion.handle)
			}
			t.Fatal(err)
		}
		assertions = append(assertions, processAssertion{pid: pid, handle: handle})
	}
	return func() {
		t.Helper()
		defer func() {
			for _, assertion := range assertions {
				_ = windows.CloseHandle(assertion.handle)
			}
		}()
		for _, assertion := range assertions {
			status, err := windows.WaitForSingleObject(assertion.handle, 0)
			if err != nil {
				t.Fatal(err)
			}
			if status != windows.WAIT_OBJECT_0 {
				t.Fatalf("process %d is still alive when Job Object shutdown returned", assertion.pid)
			}
		}
	}
}
