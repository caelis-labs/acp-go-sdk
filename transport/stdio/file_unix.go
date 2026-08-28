//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package stdio

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func duplicateFile(file *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	return os.NewFile(uintptr(fd), file.Name()), nil
}
