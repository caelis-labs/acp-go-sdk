//go:build windows

package stdio

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func assertProcessExited(t *testing.T, pid int) {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return
		}
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		t.Fatal(err)
	}
	if status == windows.WAIT_OBJECT_0 {
		return
	}
	t.Fatalf("process %d is still alive when Job Object shutdown returned", pid)
}
