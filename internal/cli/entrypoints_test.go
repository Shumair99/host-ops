package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The small cmd wrappers use os.Exit/log.Fatal; exercise the real processes so
// exit status and argument rejection are covered without changing production
// behavior solely to improve statement-coverage numbers.
func TestExecutableEntryPoints(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"+suffix), "build", "-o", dir+string(os.PathSeparator), "./cmd/...")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("entry-point build: %v %s", err, out)
	}
	for _, tc := range []struct {
		program string
		args    []string
		exit    int
		text    string
	}{
		{"hostops", []string{"dashboard", "--demo", "--once", "--color", "never"}, 0, "DEMO / FICTIONAL DATA"},
		{"hostops-agent", []string{"--config", filepath.Join(dir, "missing.json")}, 1, "open manifest"},
		{"hostops-watcher", []string{"--interval", "1s"}, 1, "interval >=5s"},
		{"hostops-vps-agent", []string{"bridge", "--socket", "/untrusted/path"}, 2, "bridge takes no arguments"},
	} {
		t.Run(tc.program, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(dir, tc.program+suffix), tc.args...)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("entry point exceeded timeout: %v", ctx.Err())
			}
			exit := 0
			if err != nil {
				if e, ok := err.(*exec.ExitError); ok {
					exit = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if exit != tc.exit || !strings.Contains(string(out), tc.text) {
				t.Fatalf("exit %d: %s", exit, out)
			}
		})
	}
}
