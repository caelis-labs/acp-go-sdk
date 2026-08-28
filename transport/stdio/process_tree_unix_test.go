//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package stdio

import (
	"errors"
	"syscall"
	"testing"
)

func assertProcessExited(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return
	}
	t.Fatalf("process %d is still alive when process-group shutdown returned", pid)
}
