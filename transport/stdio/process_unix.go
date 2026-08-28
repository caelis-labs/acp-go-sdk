//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package stdio

import (
	"errors"
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
	pid     int
	process *os.Process
	once    sync.Once
	err     error
}

func attachProcessTree(cmd *exec.Cmd) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("stdio: started process is unavailable")
	}
	return &unixProcessTree{pid: cmd.Process.Pid, process: cmd.Process}, nil
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
				if err != nil {
					t.err = err
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
		t.err = errors.Join(err, directErr)
	})
	return t.err
}

func (t *unixProcessTree) release() error { return t.terminate() }
