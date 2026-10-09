package cli

import (
	"bytes"
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteBeforeFlagsDoesNotMutateArguments(t *testing.T) {
	args := []string{"sites", "client-a", "--config", "../../examples/sites/docker.json", "--json"}
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), args, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if args[1] != "client-a" || !bytes.Contains(out.Bytes(), []byte(`"id": "client-a"`)) {
		t.Fatal("site argument lost or caller's slice mutated")
	}
}

func TestStartEnterKeepsPrivilegedServiceDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostops.json")
	if err := os.WriteFile(path, []byte(`{"host":{"id":"h","ssh":"ops@example.com"},"sites":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := RunWithInput(context.Background(), []string{"start", "--config", path, "--db", filepath.Join(dir, "history", "history.db"), "--once", "--no-collect"}, strings.NewReader("\n"), &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	c, err := config.Load(path)
	if err != nil || c.Host.HistoryMode != "session" || !strings.Contains(out.String(), "[Y/N] (default: N)") {
		t.Fatalf("unexpected default: %+v %v %s", c, err, out.String())
	}
}

func TestOfflineDashboardRequiresNoConfigOrToken(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := RunWithInput(context.Background(), []string{"dashboard", "--demo", "--once"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("DEMO / FICTIONAL DATA")) {
		t.Fatal(out.String())
	}
}

func TestHistoryPromptDefaultsAndExplicitAnswers(t *testing.T) {
	for _, tc := range []struct {
		input string
		def   bool
		want  bool
	}{
		{"\n", true, true}, {"\n", false, false}, {"yes\n", false, true}, {"N\n", true, false},
	} {
		var out bytes.Buffer
		got, err := askYesNo(strings.NewReader(tc.input), &out, "choice? ", tc.def)
		if err != nil || got != tc.want {
			t.Fatalf("answer %q default %v = %v, %v; want %v", tc.input, tc.def, got, err, tc.want)
		}
	}
}

func TestAgentUninstallDefaultsToCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hostops.json")
	if err := os.WriteFile(path, []byte(`{"host":{"id":"h","ssh":"ops@example.com"},"sites":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := RunWithInput(context.Background(), []string{"agent", "uninstall", "--config", path}, strings.NewReader("\n"), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Cancelled") {
		t.Fatal(out.String())
	}
}

func TestStartPromptsForTargetWhenSettingsAreMissing(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := RunWithInput(context.Background(), []string{"start", "--config", filepath.Join(dir, "new.json"), "--db", filepath.Join(dir, "history.db")}, strings.NewReader("\n"), &out, &errOut)
	if code != 1 || !strings.Contains(out.String(), "VPS SSH target") || !strings.Contains(errOut.String(), "SSH target is required") {
		t.Fatalf("unexpected setup response: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
