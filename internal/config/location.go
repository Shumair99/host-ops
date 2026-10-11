package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPath preserves explicit environment/project settings, while a new
// installation stores its connection in the OS user configuration directory.
func DefaultPath() (string, error) {
	if path := os.Getenv("HOSTOPS_CONFIG"); path != "" {
		return path, nil
	}
	for _, path := range []string{"hostops.json", filepath.Join("local", "hostops.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return path, nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "", fmt.Errorf("user configuration directory unavailable; pass --config FILE")
	}
	return filepath.Join(dir, "HostOps", "hostops.json"), nil
}
