//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package stdio

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureProcessCommand(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

type unixProcessTree struct {
	pid         int
	process     *os.Process
	processDone <-chan struct{}
	once        sync.Once
	err         error
}

func attachProcessTree(cmd *exec.Cmd, processDone <-chan struct{}) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("stdio: started process is unavailable")
	}
	return &unixProcessTree{pid: cmd.Process.Pid, process: cmd.Process, processDone: processDone}, nil
}

func (t *unixProcessTree) terminate() error {
	t.once.Do(func() {
		err := syscall.Kill(-t.pid, syscall.SIGKILL)
		if err == nil {
			for {
				err = syscall.Kill(-t.pid, 0)
				if errors.Is(err, syscall.ESRCH) {
					return
				}
				if errors.Is(err, syscall.EPERM) {
					// Darwin may report EPERM while the killed direct child is
					// waiting to be reaped. Recheck after the one Cmd.Wait owner
					// has observed its exit; a remaining EPERM then represents a
					// real descendant permission failure.
					<-t.processDone
					err = syscall.Kill(-t.pid, 0)
					if errors.Is(err, syscall.ESRCH) {
						return
					}
				}
				if err != nil {
					t.err = fmt.Errorf("wait for process group %d termination: %w", t.pid, err)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
		directErr := t.process.Kill()
		if errors.Is(directErr, os.ErrProcessDone) {
			directErr = nil
		}
		if errors.Is(err, syscall.ESRCH) {
			t.err = directErr
			return
		}
		t.err = errors.Join(
			fmt.Errorf("signal process group %d: %w", t.pid, err),
			directErr,
		)
	})
	return t.err
}

func (t *unixProcessTree) release() error { return t.terminate() }
