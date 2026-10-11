package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/tui"
)

func setupTestProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	t.Setenv("HOSTOPS_CONFIG", "")
	t.Setenv("APPDATA", profile)
	t.Setenv("XDG_CONFIG_HOME", profile)
	t.Setenv("HOME", profile)
	t.Chdir(t.TempDir())
	path, err := config.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFreshOnboardingThenReuseFromAnotherFolder(t *testing.T) {
	path := setupTestProfile(t)
	key := filepath.Join(t.TempDir(), "test key")
	if err := os.WriteFile(key, []byte("never copied into settings"), 0600); err != nil {
		t.Fatal(err)
	}
	var order []string
	services := setupServices{
		PrepareSSH: func(_ context.Context, c config.Config, _ io.Reader, _, _ io.Writer) error {
			order = append(order, "ssh")
			if c.Host.SSH != "ops@server.example" || c.Host.IdentityFile != key || c.Host.SSHPort != 2222 {
				t.Fatal(c)
			}
			return nil
		},
		Discover: func(_ context.Context, c config.Config, _ string) (discovery.Registry, error) {
			order = append(order, "discover")
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("saved before discovery")
			}
			return discovery.Registry{Snapshot: discovery.Snapshot{OS: "linux", Architecture: "arm64", Sources: []discovery.Source{{Kind: "podman", State: "unavailable", Issue: "permission restricted"}}, Workloads: []discovery.Workload{{ID: "app", Name: "web"}}}, Sites: []discovery.Site{{ID: "s", Domain: "example.test"}}}, nil
		},
		Dashboard: func(_ context.Context, c config.Config, _ string, options tui.Options, _ io.Reader, out io.Writer) error {
			order = append(order, "dashboard")
			if c.Host.HistoryMode != "session" || options.ConfigPath != path || options.SetupBilling {
				t.Fatal(c, options)
			}
			if loaded, err := config.Load(path); err != nil || loaded.Host.SSH != c.Host.SSH {
				t.Fatal(loaded, err)
			}
			return nil
		},
	}
	var out, errOut bytes.Buffer
	answers := "ops@server.example\n\"" + key + "\"\ninvalid\n70000\n2222\n\nn\n"
	args := []string{"--db", ":memory:", "--no-collect"}
	if code := startWithServices(context.Background(), args, strings.NewReader(answers), &out, &errOut, services); code != 0 {
		t.Fatal(code, errOut.String(), out.String())
	}
	if strings.Join(order, ",") != "ssh,discover,dashboard" {
		t.Fatal(order)
	}
	for _, text := range []string{"First-time setup", "SSH private key path (blank uses SSH config / agent)", "SSH port (blank uses SSH config / 22)", "1 addresses", "Coverage gap: podman", "Export for AI", "Ready"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("missing", text, out.String())
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Contains(data, []byte("never copied")) {
		t.Fatal("key contents saved", err)
	}
	loaded, err := config.Load(path)
	if err != nil || len(loaded.Sites) != 0 || loaded.Host.CaddyContainer != "" {
		t.Fatal("VPS-specific settings injected", loaded, err)
	}
	t.Chdir(t.TempDir())
	order = nil
	out.Reset()
	errOut.Reset()
	if code := startWithServices(context.Background(), args, strings.NewReader(""), &out, &errOut, services); code != 0 {
		t.Fatal(code, errOut.String())
	}
	if strings.Join(order, ",") != "dashboard" || strings.Contains(out.String(), "First-time setup") {
		t.Fatal(order, out.String())
	}
}

func TestOnboardingFailureDoesNotSaveUnverifiedConnection(t *testing.T) {
	for _, step := range []string{"ssh", "discovery"} {
		t.Run(step, func(t *testing.T) {
			path := setupTestProfile(t)
			services := setupServices{
				PrepareSSH: func(context.Context, config.Config, io.Reader, io.Writer, io.Writer) error {
					if step == "ssh" {
						return errors.New("SSH rejected")
					}
					return nil
				},
				Discover: func(context.Context, config.Config, string) (discovery.Registry, error) {
					return discovery.Registry{}, errors.New("discovery failed")
				},
				Dashboard: func(context.Context, config.Config, string, tui.Options, io.Reader, io.Writer) error {
					t.Fatal("opened failed setup")
					return nil
				},
			}
			var out, errOut bytes.Buffer
			if code := startWithServices(context.Background(), []string{"--db", ":memory:"}, strings.NewReader("ops@example.test\n\n\n"), &out, &errOut, services); code != 1 {
				t.Fatal(code)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("failed setup saved", err)
			}
		})
	}
}

func TestInterruptedOptionalSetupKeepsVerifiedConnection(t *testing.T) {
	path := setupTestProfile(t)
	services := setupServices{
		PrepareSSH: func(context.Context, config.Config, io.Reader, io.Writer, io.Writer) error { return nil },
		Discover: func(context.Context, config.Config, string) (discovery.Registry, error) {
			return discovery.Registry{}, nil
		},
	}
	var out, errOut bytes.Buffer
	if code := startWithServices(context.Background(), []string{"--db", ":memory:"}, strings.NewReader("ops@example.test\n\n\n"), &out, &errOut, services); code != 1 {
		t.Fatal(code)
	}
	c, err := config.Load(path)
	if err != nil || c.Host.SSH != "ops@example.test" || c.Host.HistoryMode != "" {
		t.Fatal(c, err)
	}
}

func TestInstalledCommandDemoAndVersionNeedNoSettings(t *testing.T) {
	path := setupTestProfile(t)
	for _, args := range [][]string{{"START", "--demo", "--once", "--page", "export-ai"}, {"--version"}} {
		var out, errOut bytes.Buffer
		if code := RunWithInput(context.Background(), args, strings.NewReader(""), &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		if out.Len() == 0 {
			t.Fatal("no output")
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("offline command changed settings")
	}
	var out, errOut bytes.Buffer
	if code := RunWithInput(context.Background(), nil, strings.NewReader("\n"), &out, &errOut); code != 1 || !strings.Contains(out.String(), "First-time setup") {
		t.Fatal(code, out.String(), errOut.String())
	}
}

func TestSetupSSHUsesVerifiedHostAndLiteralKeyArguments(t *testing.T) {
	h := config.Host{SSH: "ops@example.test", IdentityFile: "key with spaces", SSHPort: 2222}
	for _, interactive := range []bool{false, true} {
		args := setupSSHArgs(h, interactive)
		joined := strings.Join(args, "|")
		if !strings.Contains(joined, "-i|key with spaces") || !strings.HasSuffix(joined, "--|ops@example.test|true") || strings.Contains(joined, "StrictHostKeyChecking=no") {
			t.Fatal(args)
		}
		if interactive && !strings.Contains(joined, "StrictHostKeyChecking=ask") {
			t.Fatal(args)
		}
		if !interactive && !strings.Contains(joined, "BatchMode=yes|") {
			t.Fatal(args)
		}
	}
}
