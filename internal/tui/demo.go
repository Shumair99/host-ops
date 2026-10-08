package tui

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/deployments"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/preflight"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
	"math"
	"time"
)

func DemoConfig() config.Config {
	c := config.Config{Host: config.Host{ID: "offline-demo", Name: "Fictional host", CaddyContainer: "demo-caddy", MonthlyCost: 12, Currency: "GBP"}, Sites: []config.Site{}}
	for _, id := range []string{"alpha", "bravo"} {
		c.Sites = append(c.Sites, config.Site{ID: id, HostID: c.Host.ID, Name: "Example " + id, Repository: "example/" + id, Branch: "main", Runtime: config.Runtime{Type: "docker", Container: "demo-" + id, Port: 80, Network: "demo-web"}, Proxy: config.Proxy{Domain: id + ".example.invalid", Upstream: "demo-" + id + ":80"}, Health: config.Health{PublicURL: "https://" + id + ".example.invalid/healthz", ExpectedStatus: 200, TimeoutSeconds: 5, Path: "/healthz"}, Business: config.Business{MonthlyFee: 20}})
	}
	return c
}
func SeedDemo(ctx context.Context, c config.Config, store *storage.Store, outage bool) error {
	now := time.Now().UTC()
	reports := []model.Report{}
	for _, s := range c.Sites {
		r := model.Report{HostID: c.Host.ID, SiteID: s.ID, Name: s.Name, ObservedAt: now, Vantage: "OFFLINE DEMO - FICTIONAL", Runtime: model.Runtime{HostID: c.Host.ID, SiteID: s.ID, Type: "docker", Status: "running", Health: "healthy", Image: "demo:" + s.ID, ImageID: "demo-id", Commit: "abcdef1", User: "1000", RestartPolicy: "unless-stopped", MemoryLimit: 256 * 1024 * 1024}}
		for _, layer := range []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"} {
			r.Checks = append(r.Checks, model.Check{HostID: c.Host.ID, SiteID: s.ID, Layer: layer, State: model.Pass, Summary: "Fictional successful observation", StatusCode: 200, LatencyMS: 42})
		}
		if outage && s.ID == "bravo" {
			r.Runtime.Status = "exited"
			for i := range r.Checks {
				if r.Checks[i].Layer == "runtime" || r.Checks[i].Layer == "local_http" || r.Checks[i].Layer == "public_http" {
					r.Checks[i].State = model.Fail
					r.Checks[i].Summary = "Fictional application outage"
				}
			}
			r.Diagnoses = []model.Diagnosis{{HostID: c.Host.ID, SiteID: s.ID, Finding: "Application stopped while the proxy is available", Confidence: "high", Layer: "runtime", Evidence: []string{"Fictional runtime is exited", "Fictional DNS and TLS still pass"}, Investigation: "Inspect the runtime and recent changes; no automatic restart is performed."}}
		}
		r.Overall = model.Overall(r.Checks)
		reports = append(reports, r)
		sample := metrics.Sample{HostID: c.Host.ID, SiteID: s.ID, ObservedAt: now, Values: map[string]float64{"cpu_percent": 4, "memory_used_bytes": 64 * 1024 * 1024}, Unavailable: []string{}}
		if _, err := store.Add(ctx, c.Host.ID, s.ID, "metrics", now, sample); err != nil {
			return err
		}
	}
	if err := store.Reports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	host := metrics.Sample{HostID: c.Host.ID, ObservedAt: now, Values: map[string]float64{"cpu_percent": 12.5, "memory_percent": 37.2, "disk_percent": 88, "disk_total_bytes": 40e9, "disk_used_bytes": 35.2e9, "disk_free_bytes": 4.5e9, "memory_total_bytes": 4e9, "memory_used_bytes": 1.488e9, "uptime_seconds": 12 * 86400}, Unavailable: []string{}}
	previous, err := store.Query(ctx, c.Host.ID, "", "host_metrics", time.Time{}, 1)
	if err != nil {
		return err
	}
	if len(previous) == 0 {
		// Fictional history exists only in the explicitly labelled offline demo.
		for i := 0; i < 60; i++ {
			sample := host
			sample.ObservedAt = now.Add(time.Duration(i-60) * 15 * time.Second)
			sample.Values = map[string]float64{"cpu_percent": 12.5 + 8*math.Sin(float64(i)*0.28) + 4*math.Sin(float64(i)*0.73), "memory_percent": 37.2 + 5*math.Sin(float64(i)*0.12), "disk_percent": 86 + float64(i)/30}
			if _, err := store.Add(ctx, c.Host.ID, "", "host_metrics", sample.ObservedAt, sample); err != nil {
				return err
			}
		}
	}
	if _, err := store.Add(ctx, c.Host.ID, "", "host_metrics", now, host); err != nil {
		return err
	}
	status := "healthy"
	if outage {
		status = "critical"
	}
	if _, err := store.Add(ctx, c.Host.ID, "bravo", "event", now, map[string]string{"summary": "DEMO scenario: bravo is " + status, "after": status}); err != nil {
		return err
	}
	if _, err := store.AddOnce(ctx, c.Host.ID, "alpha", "deployment", "demo-deploy", now, deployments.Deployment{HostID: c.Host.ID, SiteID: "alpha", ID: "demo-deploy", Status: "completed", Commit: "abcdef1", HealthResult: "healthy", Origin: "offline_demo", ObservedAt: now}); err != nil {
		return err
	}
	cycles := 2
	if outage {
		cycles = 3
	}
	for cycle := 0; cycle < cycles; cycle++ {
		if _, err := incidents.Observe(ctx, store, reports[1], "demo", 3, 2); err != nil {
			return err
		}
	}
	return nil
}
func DemoDisk() metrics.DiskReport {
	return metrics.DiskReport{HostID: "offline-demo", ObservedAt: time.Now().UTC(), Directories: []metrics.DiskEntry{{Path: "/var/log (fictional)", Bytes: 3e9, Partial: true}, {Path: "/srv/apps (fictional)", Bytes: 700e6}}, Docker: []map[string]string{{"Type": "Build Cache", "Size": "6.7 GB", "Reclaimable": "4.0 GB (fictional)"}}, Unavailable: []string{"demo illustrates incomplete permission-limited directory totals"}}
}
func DemoSecurity() security.Report {
	return security.Report{HostID: "offline-demo", ObservedAt: time.Now().UTC(), Findings: []security.Finding{{SiteID: "alpha", State: model.Pass, Title: "Fictional non-root user", Evidence: "1000"}, {SiteID: "bravo", State: model.Warn, Title: "Fictional writable root filesystem", Investigation: "Review whether read-only execution is practical."}}, Exposures: []security.Exposure{{Container: "demo-caddy", HostPort: "443", State: model.Pass, Evidence: "Fictional proxy publication"}}, Unavailable: []string{"Offline demo; no real socket, firewall or Docker inspection occurred"}}
}
func DemoPreflight(c config.Config) preflight.Report {
	impact, _ := preflight.Dependency(c, "host")
	checks := []model.Check{{Layer: "docker_boot", State: model.Pass, Summary: "Fictional boot policy"}, {Layer: "backup_freshness", State: model.Unknown, Summary: "No backup report provided"}, {Layer: "host_capacity", State: model.Warn, Summary: "Fictional disk above 85%"}}
	return preflight.Report{HostID: c.Host.ID, Target: "reboot", ObservedAt: time.Now().UTC(), Overall: model.Overall(checks), Recommendation: preflight.Recommendation(checks), Checks: checks, Impact: impact}
}
