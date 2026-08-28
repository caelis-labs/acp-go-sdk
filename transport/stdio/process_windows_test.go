//go:build windows

package stdio

import (
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestConfigureProcessCommandHidesWindowsConsole(t *testing.T) {
	cmd := exec.Command("acp-agent.exe")
	configureProcessCommand(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow is false")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&createSuspended == 0 {
		t.Fatalf("CreationFlags = %#x, want CREATE_SUSPENDED", cmd.SysProcAttr.CreationFlags)
	}
}

func TestJobProcessIDListHeaderLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(jobBasicProcessIDListHeader{}), uintptr(8); got != want {
		t.Fatalf("job process ID list header size = %d, want %d", got, want)
	}
}

func TestProcessInJobRejectsUnrelatedProcess(t *testing.T) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)
	inJob, err := processInJob(windows.CurrentProcess(), job)
	if err != nil {
		t.Fatal(err)
	}
	if inJob {
		t.Fatal("current test process unexpectedly belongs to the empty child job")
	}
}
