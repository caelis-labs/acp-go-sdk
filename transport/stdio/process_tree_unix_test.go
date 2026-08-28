//go:build aix || dragonfly || freebsd || illumos || netbsd || openbsd || solaris

package stdio

import (
	"errors"
	"syscall"
	"testing"
)

func captureProcessExitAssertions(t *testing.T, pids []int) func() {
	t.Helper()
	return func() {
		t.Helper()
		for _, pid := range pids {
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("process %d is still alive when process-group shutdown returned", pid)
			}
		}
	}
}
