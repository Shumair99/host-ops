package health

import (
	"context"
	"github.com/Shumair99/host-ops/internal/caddy"
	"github.com/Shumair99/host-ops/internal/diagnostics"
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
	checks = append(checks, LocalHTTP(ctx, runner, s, r))
	if s.Proxy.Domain != "" {
		checks = append(checks, caddy.Inspect(ctx, runner, c.Host, s)...)
	}
	checks = append(checks, Network(ctx, s)...)
	checks = append(checks, HTTP(ctx, s, s.Health.PublicURL, "public_http"))
	report := model.Report{HostID: s.HostID, SiteID: s.ID, Name: s.Name, ObservedAt: time.Now().UTC(), Vantage: vantage, Runtime: r, Checks: checks, Overall: model.Overall(checks)}
	report.Diagnoses = diagnostics.Diagnose(report)
	return report
}
