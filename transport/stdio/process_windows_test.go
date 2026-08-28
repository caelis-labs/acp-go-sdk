//go:build windows

package stdio

import (
	"os/exec"
	"testing"
	"unsafe"
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

func TestJobBasicAccountingInformationLayout(t *testing.T) {
	if got, want := unsafe.Sizeof(jobBasicAccountingInformation{}), uintptr(48); got != want {
		t.Fatalf("job accounting information size = %d, want %d", got, want)
	}
}
