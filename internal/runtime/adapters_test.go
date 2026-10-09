package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

type adapterCall struct {
	command string
	args    []string
}

type adapterRunner struct {
	calls       []adapterCall
	staticFails map[string]bool
	unitOutput  []byte
	unitErr     error
}

func (r *adapterRunner) Run(_ context.Context, command string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, adapterCall{command: command, args: append([]string(nil), args...)})
	switch command {
	case "test":
		if len(args) != 2 {
			return nil, errors.New("unexpected test arguments")
		}
		if r.staticFails[args[1]] {
			return nil, errors.New("not found or inaccessible")
		}
		return nil, nil
	case "systemctl":
		return r.unitOutput, r.unitErr
	default:
		return nil, errors.New("unexpected command")
	}
}

func TestForSelectsOnlySupportedAdapters(t *testing.T) {
	for _, kind := range []string{"docker", "static", "systemd"} {
		if For(kind) == nil {
			t.Errorf("For(%q) returned nil", kind)
		}
	}
	for _, kind := range []string{"", "podman", "shell"} {
		if For(kind) != nil {
			t.Errorf("For(%q) returned an unsupported adapter", kind)
		}
	}
}

func TestStaticAdapterKeepsMissingEvidenceUnknown(t *testing.T) {
	site := config.Site{
		ID:     "site-1",
		HostID: "host-1",
		Runtime: config.Runtime{
			Directory:     "/srv/my site",
			ExpectedFiles: []string{"index.html", "asset bundle/app.js"},
		},
	}
	runner := &adapterRunner{staticFails: map[string]bool{"/srv/my site/asset bundle/app.js": true}}
	runtime, checks := (Static{}).Inspect(context.Background(), runner, site)
	if runtime.Status != "unknown" || runtime.Type != "static" || runtime.Name != site.Runtime.Directory {
		t.Fatalf("unexpected static runtime: %+v", runtime)
	}
	if len(checks) != 3 || checks[0].State != model.Pass || checks[1].State != model.Pass || checks[2].State != model.Unknown {
		t.Fatalf("missing file did not remain unknown: %+v", checks)
	}
	want := []adapterCall{
		{command: "test", args: []string{"-d", "/srv/my site"}},
		{command: "test", args: []string{"-f", "/srv/my site/index.html"}},
		{command: "test", args: []string{"-f", "/srv/my site/asset bundle/app.js"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("static probe arguments = %#v, want %#v", runner.calls, want)
	}
}

func TestSystemdAdapterSeparatesActiveAndBootState(t *testing.T) {
	site := config.Site{ID: "site-1", HostID: "host-1", Runtime: config.Runtime{Unit: "web.service"}}
	t.Run("active and enabled", func(t *testing.T) {
		runner := &adapterRunner{unitOutput: []byte("LoadState=loaded\nActiveState=active\nUnitFileState=enabled\nRestart=on-failure\n")}
		runtime, checks := (Systemd{}).Inspect(context.Background(), runner, site)
		if runtime.Status != "active" || runtime.RestartPolicy != "on-failure" || len(checks) != 1 || checks[0].State != model.Pass {
			t.Fatalf("unexpected active unit evidence: runtime=%+v checks=%+v", runtime, checks)
		}
		if got, want := runner.calls[0].args, []string{"show", "--property=LoadState,ActiveState,UnitFileState,Restart", "--", "web.service"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("systemctl arguments = %#v, want %#v", got, want)
		}
	})

	t.Run("inactive and disabled", func(t *testing.T) {
		runner := &adapterRunner{unitOutput: []byte("LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n")}
		_, checks := (Systemd{}).Inspect(context.Background(), runner, site)
		if len(checks) != 2 || checks[0].State != model.Fail || checks[1].State != model.Warn {
			t.Fatalf("inactive or disabled unit was misclassified: %+v", checks)
		}
	})

	t.Run("unavailable remains unknown", func(t *testing.T) {
		runner := &adapterRunner{unitErr: errors.New("permission denied")}
		_, checks := (Systemd{}).Inspect(context.Background(), runner, site)
		if len(checks) != 1 || checks[0].State != model.Unknown {
			t.Fatalf("inaccessible unit became a confirmed failure: %+v", checks)
		}
	})
}
