//go:build !windows && !android && !darwin && !dragonfly && !freebsd && !illumos && !ios && !linux && !netbsd && !openbsd && !solaris

package stdio

import (
	"fmt"
	"os"
	"runtime"
)

func duplicateFile(*os.File) (*os.File, error) {
	return nil, fmt.Errorf("stdio: duplicate file is unsupported on %s", runtime.GOOS)
}
