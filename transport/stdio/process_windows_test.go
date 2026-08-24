//go:build windows

package stdio

import (
	"os/exec"
	"testing"
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
}
