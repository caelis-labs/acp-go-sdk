//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package stdio

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func duplicateFile(file *os.File) (*os.File, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	var fd int
	var duplicateErr error
	if err := raw.Control(func(source uintptr) {
		fd, duplicateErr = unix.FcntlInt(source, unix.F_DUPFD_CLOEXEC, 0)
	}); err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	if duplicateErr != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", duplicateErr)
	}
	return os.NewFile(uintptr(fd), file.Name()), nil
}
