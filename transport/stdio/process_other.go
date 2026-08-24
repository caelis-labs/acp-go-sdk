//go:build !windows

package stdio

import "os/exec"

func configureProcessCommand(*exec.Cmd) {}
