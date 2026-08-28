//go:build darwin

package stdio

import (
	"testing"

	"golang.org/x/sys/unix"
)

type darwinProcessIdentity struct {
	pid       int
	startTime unix.Timeval
}

func captureProcessExitAssertions(t *testing.T, pids []int) func() {
	t.Helper()
	identities := make([]darwinProcessIdentity, 0, len(pids))
	for _, pid := range pids {
		process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			t.Fatal(err)
		}
		identities = append(identities, darwinProcessIdentity{
			pid:       pid,
			startTime: process.Proc.P_starttime,
		})
	}
	return func() {
		t.Helper()
		for _, identity := range identities {
			process, err := unix.SysctlKinfoProc("kern.proc.pid", identity.pid)
			if err != nil {
				continue
			}
			if process.Proc.P_starttime != identity.startTime {
				// The original process exited and its PID was reused.
				continue
			}
			t.Fatalf("process %d is still alive when process-group shutdown returned", identity.pid)
		}
	}
}
