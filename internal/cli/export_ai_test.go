package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportAICommandSavesOfflineDemoAndPreservesExistingExport(t *testing.T) {
	file := filepath.Join(t.TempDir(), "review.md")
	args := []string{"export-ai", "--demo", "--output", file, "--json"}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	var result struct {
		Path  string
		Views int
		Demo  bool
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Path != file || result.Views != 19 || !result.Demo {
		t.Fatal("incorrect export result", out.String(), err)
	}
	first, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(first), " | Preflight | ") || !strings.Contains(string(first), "## AI review prompt") {
		t.Fatal("incomplete exported report", err)
	}
	out.Reset()
	errOut.Reset()
	if code := Run(context.Background(), args, &out, &errOut); code != 1 {
		t.Fatal("existing export was overwritten")
	}
	second, _ := os.ReadFile(file)
	if !bytes.Equal(first, second) {
		t.Fatal("existing export changed")
	}
}
