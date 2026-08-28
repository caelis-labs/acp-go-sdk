package stdio

import (
	"errors"
	"os"
)

// DuplicateFile returns an independently closable handle to file. The
// duplicate is not inherited by subsequently started child processes.
func DuplicateFile(file *os.File) (*os.File, error) {
	if file == nil {
		return nil, errors.New("stdio: file is required")
	}
	return duplicateFile(file)
}
