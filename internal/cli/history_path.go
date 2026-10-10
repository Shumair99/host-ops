package cli

import (
	"fmt"
	"io"

	"github.com/Shumair99/host-ops/internal/storage"
)

func resolveHistoryPath(path string, errOut io.Writer) (string, bool) {
	if path != "" {
		return path, true
	}
	path, err := storage.DefaultHistoryPath()
	if err != nil {
		fmt.Fprintf(errOut, "Could not locate private history storage: %v\n", err)
		return "", false
	}
	return path, true
}
