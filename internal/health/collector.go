package health

import (
	"context"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	runtimecheck "github.com/Shumair99/host-ops/internal/runtime"
)

func Collect(ctx context.Context, c config.Config, s config.Site, runner platform.Runner) model.Report {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	r, checks := runtimecheck.For(s.Runtime.Type).Inspect(ctx, runner, s)
	vantage := "host-local"
	if c.Host.SSH != "" {
		vantage = "remote-host over SSH"
	}
	return model.Report{HostID: s.HostID, SiteID: s.ID, Name: s.Name, ObservedAt: time.Now().UTC(), Vantage: vantage, Runtime: r, Checks: checks, Overall: model.Overall(checks), Diagnoses: []model.Diagnosis{}}
}
