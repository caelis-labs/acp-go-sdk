//go:build windows

package stdio

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func duplicateFile(file *os.File) (*os.File, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	var duplicateErr error
	if err := raw.Control(func(source uintptr) {
		duplicateErr = windows.DuplicateHandle(
			process,
			windows.Handle(source),
			process,
			&duplicate,
			0,
			false,
			windows.DUPLICATE_SAME_ACCESS,
		)
	}); err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	if duplicateErr != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", duplicateErr)
	}
	return os.NewFile(uintptr(duplicate), file.Name()), nil
}
