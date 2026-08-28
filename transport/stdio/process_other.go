//go:build !windows && !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris

package stdio

import "os/exec"

func configureProcessCommand(*exec.Cmd) {}

func attachProcessTree(cmd *exec.Cmd, _ <-chan struct{}) (processTree, error) {
	return directProcessTree{process: cmd.Process}, nil
}
