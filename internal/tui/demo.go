package tui

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/deployments"
	"github.com/Shumair99/host-ops/internal/discovery"
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
	for _, id := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"} {
		c.Sites = append(c.Sites, config.Site{ID: id, HostID: c.Host.ID, Name: "Example " + id, Repository: "example/" + id, Branch: "main", Runtime: config.Runtime{Type: "docker", Container: "demo-" + id, Port: 80, Network: "demo-web"}, Proxy: config.Proxy{Domain: id + ".example.invalid", Upstream: "demo-" + id + ":80"}, Health: config.Health{PublicURL: "https://" + id + ".example.invalid/healthz", ExpectedStatus: 200, TimeoutSeconds: 5, Path: "/healthz"}, Business: config.Business{MonthlyFee: 20}})
	}
	return c
}
func SeedDemo(ctx context.Context, c config.Config, store *storage.Store, outage bool) error {
	now := time.Now().UTC()
	reports := []model.Report{}
	for index, s := range c.Sites {
		r := model.Report{HostID: c.Host.ID, SiteID: s.ID, Name: s.Name, ObservedAt: now, Vantage: "OFFLINE DEMO - FICTIONAL", Runtime: model.Runtime{PostureObserved: true, HostID: c.Host.ID, SiteID: s.ID, Type: "docker", Name: s.Runtime.Container, Status: "running", StartedAt: now.Add(-72 * time.Hour).Format(time.RFC3339), Health: "healthy", Image: "demo:" + s.ID, ImageID: "demo-id", Commit: "abcdef1", User: "1000", RestartPolicy: "unless-stopped", MemoryLimit: 256 * 1024 * 1024}}
		for _, layer := range []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"} {
			check := model.Check{HostID: c.Host.ID, SiteID: s.ID, Layer: layer, State: model.Pass, Summary: "Fictional successful observation"}
			if layer == "local_http" || layer == "public_http" {
				check.StatusCode = 200
				check.LatencyMS = 28.4 + float64(index*index)*12.3
			}
			r.Checks = append(r.Checks, check)
		}
		if outage && s.ID == "bravo" {
			r.Runtime.Status = "exited"
			for i := range r.Checks {
				if r.Checks[i].Layer == "runtime" || r.Checks[i].Layer == "local_http" || r.Checks[i].Layer == "public_http" {
					r.Checks[i].State = model.Fail
					r.Checks[i].Summary = "Fictional application outage"
					if r.Checks[i].Layer == "local_http" {
						r.Checks[i].StatusCode, r.Checks[i].LatencyMS = 0, 0
					} else if r.Checks[i].Layer == "public_http" {
						r.Checks[i].StatusCode = 503
					}
				}
			}
			r.Diagnoses = []model.Diagnosis{{HostID: c.Host.ID, SiteID: s.ID, Finding: "Application stopped while the proxy is available", Confidence: "high", Layer: "runtime", Evidence: []string{"Fictional runtime is exited", "Fictional DNS and TLS still pass"}, Investigation: "Inspect the runtime and recent changes; no automatic restart is performed."}}
		}
		r.Overall = model.Overall(r.Checks)
		reports = append(reports, r)
		sample := metrics.Sample{HostID: c.Host.ID, SiteID: s.ID, ObservedAt: now, Values: map[string]float64{"cpu_percent": 2.7 + float64(index)*2.3, "memory_used_bytes": float64(index+1) * 43.7e6}, Unavailable: []string{}}
		if outage && s.ID == "bravo" {
			sample.Values = map[string]float64{}
			sample.Unavailable = []string{"Fictional stopped workload has no current stats"}
		}
		previous, err := store.Query(ctx, c.Host.ID, s.ID, "metrics", time.Time{}, 1)
		if err != nil {
			return err
		}
		if len(previous) == 0 {
			for i := 0; i < 60; i++ {
				historic := sample
				historic.ObservedAt = now.Add(time.Duration(i-60) * 30 * time.Second)
				historic.Values = map[string]float64{"cpu_percent": max(0, .9+float64(index)*2.3+2*math.Sin(float64(i)*.24)), "memory_used_bytes": float64(index+1) * 43.7e6 * (1 + .08*math.Sin(float64(i)*.15))}
				if _, err := store.Add(ctx, c.Host.ID, s.ID, "metrics", historic.ObservedAt, historic); err != nil {
					return err
				}
			}
		}
		if _, err := store.Add(ctx, c.Host.ID, s.ID, "metrics", now, sample); err != nil {
			return err
		}
	}
	if err := store.Reports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	host := metrics.Sample{HostID: c.Host.ID, ObservedAt: now, Values: demoHostValues(120), Unavailable: []string{}}
	previous, err := store.Query(ctx, c.Host.ID, "", "host_metrics", time.Time{}, 1)
	if err != nil {
		return err
	}
	if len(previous) == 0 {
		// Fictional history exists only in the explicitly labelled offline demo.
		for i := 0; i < 120; i++ {
			sample := host
			sample.ObservedAt = now.Add(time.Duration(i-120) * 15 * time.Second)
			sample.Values = demoHostValues(i)
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
func demoHostValues(i int) map[string]float64 {
	x := float64(i)
	memory := 37.2 + 5*math.Sin(x*.12)
	return map[string]float64{
		"cpu_percent":                  12.5 + 8*math.Sin(x*.28) + 4*math.Sin(x*.73),
		"cpu_count":                    4,
		"memory_percent":               memory,
		"memory_total_bytes":           4e9,
		"memory_used_bytes":            memory / 100 * 4e9,
		"memory_available_bytes":       (100 - memory) / 100 * 4e9,
		"memory_cached_bytes":          .63e9,
		"swap_used_bytes":              22.4e6,
		"swap_total_bytes":             1e9,
		"uptime_seconds":               12*86400 + x*15,
		"load_1":                       .62 + .18*math.Sin(x*.17),
		"load_5":                       .48,
		"load_15":                      .39,
		"tasks_running":                2,
		"tasks_blocked":                0,
		"tasks_total":                  184,
		"pressure_cpu_some_percent":    1.2,
		"pressure_memory_some_percent": .04,
		"pressure_io_some_percent":     .27,
		"network_interfaces":           1,
		"network_interface_identity":   1,
		"network_rx_bytes":             4e9 + x*240e3 + 150e3*math.Sin(x*.2),
		"network_tx_bytes":             1.9e9 + x*110e3 + 60e3*math.Sin(x*.18),
		"network_rx_packets":           3.2e6 + x*360,
		"network_tx_packets":           1.6e6 + x*190,
		"network_rx_errors":            0,
		"network_tx_errors":            0,
		"network_rx_dropped":           3,
		"network_tx_dropped":           0,
		"disk_percent":                 86 + x/60,
		"disk_total_bytes":             40e9,
		"disk_used_bytes":              35.2e9,
		"disk_free_bytes":              4.5e9,
	}
}

func DemoInventory(c config.Config) discovery.Registry {
	at := time.Now().UTC()
	r := discovery.Registry{Snapshot: discovery.Snapshot{HostID: c.Host.ID, ObservedAt: at, OS: "Linux (fictional)", Architecture: "amd64", Sources: []discovery.Source{{ID: "demo-docker", Kind: "runtime", State: "ready", Mode: "active"}, {ID: "demo-caddy", Kind: "proxy", State: "ready", Mode: "active"}}, Listeners: []discovery.Listener{{Address: ":443"}, {Address: ":80"}}}}
	for _, s := range c.Sites {
		route := discovery.Route{ID: "demo-route-" + s.ID, SourceID: "demo-caddy", Hosts: []string{s.Proxy.Domain}, Kind: "proxy", Scheme: "https", Upstreams: []string{s.Proxy.Upstream}}
		r.Snapshot.Workloads = append(r.Snapshot.Workloads, discovery.Workload{ID: s.ID, SourceID: "demo-docker", Name: s.Runtime.Container, Type: "docker", Status: "running", Image: "demo:" + s.ID})
		r.Snapshot.Routes = append(r.Snapshot.Routes, route)
		r.Sites = append(r.Sites, discovery.Site{ID: s.ID, Domain: s.Proxy.Domain, Routes: []discovery.Route{route}, WorkloadIDs: []string{s.ID}, State: "active"})
	}
	return r
}

func DemoDisk() metrics.DiskReport {
	alpha, bravo, writable := int64(300e6), int64(400e6), int64(8e6)
	return metrics.DiskReport{HostID: "offline-demo", ObservedAt: time.Now().UTC(), DurationSeconds: 2.4, Roots: []string{"/"}, Filesystems: []metrics.Filesystem{{Mount: "/", Type: "ext4", TotalBytes: 40e9, UsedBytes: 35.2e9, AvailableBytes: 4.5e9, Inodes: 1000000, InodesUsed: 40000}}, Sites: []metrics.SiteStorage{{SiteID: "alpha", Name: "alpha.example.invalid", DirectoryBytes: &alpha, WritableBytes: &writable}, {SiteID: "bravo", Name: "bravo.example.invalid", DirectoryBytes: &bravo, Shared: true}}, Directories: []metrics.DiskEntry{{Path: "/", Bytes: 35.2e9, Partial: true}, {Path: "/var", Parent: "/", Bytes: 13e9, Partial: true}, {Path: "/var/log (fictional)", Parent: "/var", Bytes: 3e9, Partial: true}, {Path: "/srv", Parent: "/", Bytes: 700e6}, {Path: "/srv/apps (fictional)", Parent: "/srv", Bytes: 700e6}}, Docker: []map[string]string{{"Type": "Images", "Size": "12.0 GB", "Reclaimable": "1.0 GB (fictional)"}, {"Type": "Build Cache", "Size": "6.7 GB", "Reclaimable": "4.0 GB (fictional)"}}, Unavailable: []string{"demo illustrates incomplete permission-limited directory totals"}}
}
func DemoSecurity() security.Report {
	return security.Report{HostID: "offline-demo", ObservedAt: time.Now().UTC(), Findings: []security.Finding{{SiteID: "alpha", State: model.Pass, Title: "Fictional non-root user", Evidence: "1000"}, {SiteID: "bravo", State: model.Warn, Title: "Fictional writable root filesystem", Investigation: "Review whether read-only execution is practical."}}, Exposures: []security.Exposure{{Container: "demo-caddy", HostPort: "443", State: model.Pass, Evidence: "Fictional proxy publication"}}, Unavailable: []string{"Offline demo; no real socket, firewall or Docker inspection occurred"}}
}
func DemoPreflight(c config.Config) preflight.Report {
	impact, _ := preflight.Dependency(c, "host")
	checks := []model.Check{{Layer: "docker_boot", State: model.Pass, Summary: "Fictional boot policy"}, {Layer: "backup_freshness", State: model.Unknown, Summary: "No backup report provided"}, {Layer: "host_capacity", State: model.Warn, Summary: "Fictional disk above 85%"}}
	return preflight.Report{HostID: c.Host.ID, Target: "reboot", ObservedAt: time.Now().UTC(), Overall: model.Overall(checks), Recommendation: preflight.Recommendation(checks), Checks: checks, Impact: impact}
}
