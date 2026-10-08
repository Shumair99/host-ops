package runtime

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

type Adapter interface {
	Inspect(context.Context, platform.Runner, config.Site) (model.Runtime, []model.Check)
}

type Docker struct{}
type Static struct{}
type Systemd struct{}

func For(kind string) Adapter {
	switch kind {
	case "docker":
		return Docker{}
	case "static":
		return Static{}
	case "systemd":
		return Systemd{}
	}
	return nil
}

func (Docker) Inspect(ctx context.Context, runner platform.Runner, s config.Site) (model.Runtime, []model.Check) {
	return InspectDocker(ctx, runner, s)
}

func (Static) Inspect(ctx context.Context, runner platform.Runner, s config.Site) (model.Runtime, []model.Check) {
	r := model.Runtime{HostID: s.HostID, SiteID: s.ID, Type: "static", Name: s.Runtime.Directory}
	checks := []model.Check{}
	check := func(name, path string, isDir bool) {
		flag := "-f"
		if isDir {
			flag = "-d"
		}
		_, err := runner.Run(ctx, "test", flag, path)
		state, summary := model.Pass, name+" exists"
		// The command runner cannot distinguish ENOENT from a denied/failed probe.
		if err != nil {
			state, summary = model.Unknown, name+" missing or inaccessible"
		}
		checks = append(checks, model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "runtime", State: state, Summary: summary})
	}
	check("Deployment directory", s.Runtime.Directory, true)
	for _, name := range s.Runtime.ExpectedFiles {
		check(name, filepath.ToSlash(filepath.Join(s.Runtime.Directory, name)), false)
	}
	r.Status = "present"
	if model.Overall(checks) != "healthy" {
		r.Status = "unknown"
	}
	return r, checks
}

func (Systemd) Inspect(ctx context.Context, runner platform.Runner, s config.Site) (model.Runtime, []model.Check) {
	r := model.Runtime{HostID: s.HostID, SiteID: s.ID, Type: "systemd", Name: s.Runtime.Unit}
	check := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "runtime", State: model.Unknown, Summary: "Unit unavailable or access denied"}
	data, err := runner.Run(ctx, "systemctl", "show", "--property=LoadState,ActiveState,UnitFileState,Restart", "--", s.Runtime.Unit)
	if err != nil {
		return r, []model.Check{check}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	r.Status, r.RestartPolicy = values["ActiveState"], values["Restart"]
	check.State, check.Summary = model.Pass, r.Status
	if values["LoadState"] != "loaded" || r.Status != "active" {
		check.State = model.Fail
	}
	checks := []model.Check{check}
	if values["UnitFileState"] != "enabled" {
		checks = append(checks, model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "restart_policy", State: model.Warn, Summary: "Unit is not enabled at boot"})
	}
	return r, checks
}
