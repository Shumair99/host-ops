//go:build !windows

package aiexport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "review.md")
	if _, err := Write(context.Background(), path, []byte("private evidence")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("export is not private", err)
	}
}
