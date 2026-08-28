//go:build linux

package stdio

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func captureProcessExitAssertions(t *testing.T, pids []int) func() {
	t.Helper()
	type processAssertion struct {
		pid int
		fd  int
	}
	assertions := make([]processAssertion, 0, len(pids))
	for _, pid := range pids {
		fd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ENOSYS) {
			for _, assertion := range assertions {
				_ = unix.Close(assertion.fd)
			}
			t.Skip("pidfd is unavailable on this Linux kernel")
		}
		if err != nil {
			for _, assertion := range assertions {
				_ = unix.Close(assertion.fd)
			}
			t.Fatal(err)
		}
		assertions = append(assertions, processAssertion{pid: pid, fd: fd})
	}
	return func() {
		t.Helper()
		pollFDs := make([]unix.PollFd, len(assertions))
		for index, assertion := range assertions {
			pollFDs[index] = unix.PollFd{Fd: int32(assertion.fd), Events: unix.POLLIN}
		}
		_, pollErr := unix.Poll(pollFDs, 0)
		for _, assertion := range assertions {
			_ = unix.Close(assertion.fd)
		}
		if pollErr != nil {
			t.Fatal(pollErr)
		}
		for index, pollFD := range pollFDs {
			if pollFD.Revents&unix.POLLIN == 0 {
				t.Fatalf("process %d is still alive when process-group shutdown returned", assertions[index].pid)
			}
		}
	}
}
