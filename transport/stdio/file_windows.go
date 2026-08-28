//go:build windows

package stdio

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func duplicateFile(file *os.File) (*os.File, error) {
	process := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(
		process,
		windows.Handle(file.Fd()),
		process,
		&duplicate,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	); err != nil {
		return nil, fmt.Errorf("stdio: duplicate file: %w", err)
	}
	return os.NewFile(uintptr(duplicate), file.Name()), nil
}
