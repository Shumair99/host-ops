//go:build !windows

package aiexport

import "os"

func protectFile(path string) error { return os.Chmod(path, 0600) }
