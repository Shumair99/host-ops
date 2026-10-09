package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// sourceRoot locates a checkout for go run and contributor builds. Packaged
// releases do not need source files because their platform-specific assets are
// shipped beside the executable.
func sourceRoot() string {
	starts := make([]string, 0, 2)
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(file))
	}
	for _, start := range starts {
		if root := findSourceRoot(start); root != "" {
			return root
		}
	}
	return ""
}

func findSourceRoot(start string) string {
	root, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for depth := 0; depth < 32; depth++ {
		if regularFile(filepath.Join(root, "go.mod")) &&
			regularFile(filepath.Join(root, "cmd", "hostops", "main.go")) &&
			regularFile(filepath.Join(root, "cmd", "hostops-vps-agent", "main.go")) {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return ""
}

func regularFile(name string) bool {
	info, err := os.Lstat(name)
	return err == nil && info.Mode().IsRegular()
}

func bundledAsset(relative string) string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	name := filepath.Join(filepath.Dir(executable), filepath.FromSlash(relative))
	if regularFile(name) {
		return name
	}
	return ""
}

// sourceAsset returns the normal local build location used by scripts/check.ps1.
func sourceAsset(relative string) string {
	root := sourceRoot()
	if root == "" {
		return ""
	}
	name := filepath.Join(root, "bin", filepath.FromSlash(relative))
	if regularFile(name) {
		return name
	}
	return ""
}

// buildSourceAsset creates a missing cross-compiled helper only when HostOps is
// being run from its source checkout. Release users use the adjacent bundled
// artifact and never need a Go toolchain.
func buildSourceAsset(ctx context.Context, relative, packagePath, architecture, description string) (string, error) {
	root := sourceRoot()
	if root == "" {
		return "", fmt.Errorf("%s is not in this release bundle; run from the HostOps source checkout or use a release bundle", description)
	}
	output := filepath.Join(root, "bin", filepath.FromSlash(relative))
	return buildSourceArtifact(ctx, root, output, packagePath, architecture, description)
}

func buildSourceArtifact(ctx context.Context, root, output, packagePath, architecture, description string) (string, error) {
	if regularFile(output) {
		return output, nil
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("%s is missing and Go was not found on PATH; install Go or use a release bundle", description)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return "", fmt.Errorf("prepare local %s build directory: %w", description, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".hostops-build-*.tmp")
	if err != nil {
		return "", fmt.Errorf("prepare local %s build: %w", description, err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("prepare local %s build: %w", description, err)
	}
	defer os.Remove(tmpPath)

	buildCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, goPath, "build", "-buildvcs=false", "-trimpath", "-o", tmpPath, packagePath)
	cmd.Dir = root
	cmd.Env = crossCompileEnvironment(architecture)
	outputText, buildErr := cmd.CombinedOutput()
	if buildErr != nil {
		detail := strings.TrimSpace(string(outputText))
		if len(detail) > 3000 {
			detail = detail[:3000] + "…"
		}
		if buildCtx.Err() != nil {
			return "", fmt.Errorf("building local %s exceeded the 3-minute limit", description)
		}
		if detail != "" {
			return "", fmt.Errorf("build local %s: %s", description, detail)
		}
		return "", fmt.Errorf("build local %s: %w", description, buildErr)
	}
	if err := os.Rename(tmpPath, output); err != nil {
		// Another HostOps process may have completed the same build concurrently.
		if regularFile(output) {
			return output, nil
		}
		return "", fmt.Errorf("publish local %s build: %w", description, err)
	}
	return output, nil
}

func crossCompileEnvironment(architecture string) []string {
	values := map[string]string{
		"GOOS":        "linux",
		"GOARCH":      architecture,
		"CGO_ENABLED": "0",
	}
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := values[strings.ToUpper(name)]; !replaced {
			env = append(env, entry)
		}
	}
	for _, key := range []string{"GOOS", "GOARCH", "CGO_ENABLED"} {
		env = append(env, key+"="+values[key])
	}
	return env
}
