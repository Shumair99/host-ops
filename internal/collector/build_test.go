package collector

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSourceRootFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"go.mod",
		"cmd/hostops/main.go",
		"cmd/hostops-vps-agent/main.go",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(root, "internal", "collector")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if got := findSourceRoot(nested); got != root {
		t.Fatalf("findSourceRoot(%q) = %q, want %q", nested, got, root)
	}
}

func TestFindSourceRootRejectsPartialCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := findSourceRoot(root); got != "" {
		t.Fatalf("findSourceRoot accepted incomplete checkout %q", got)
	}
}

func TestCrossCompileEnvironmentOverridesTargetsOnce(t *testing.T) {
	counts := map[string]int{}
	values := map[string]string{}
	for _, entry := range crossCompileEnvironment("arm64") {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(key)
		if key == "GOOS" || key == "GOARCH" || key == "CGO_ENABLED" {
			counts[key]++
			values[key] = value
		}
	}
	for key, want := range map[string]string{"GOOS": "linux", "GOARCH": "arm64", "CGO_ENABLED": "0"} {
		if counts[key] != 1 || values[key] != want {
			t.Errorf("%s occurs %d times with value %q; want once with %q", key, counts[key], values[key], want)
		}
	}
}

func TestBuildSourceArtifactCrossCompilesVPSAgent(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("Go is not on PATH")
	}
	root := sourceRoot()
	if root == "" {
		t.Skip("test is not running from a HostOps source checkout")
	}
	output := filepath.Join(t.TempDir(), "hostops-vps-agent")
	artifact, err := buildSourceArtifact(context.Background(), root, output, "./cmd/hostops-vps-agent", "amd64", "test VPS history agent")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateLinuxExecutable(data, "amd64"); err != nil {
		t.Fatalf("cross-compiled VPS agent is invalid: %v", err)
	}
}
