//go:build !windows

package config

import "os"

func protectSettings(path string) error { return os.Chmod(path, 0600) }
