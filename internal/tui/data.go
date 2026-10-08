package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/preflight"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"time"
)

type Snapshot struct {
	Sites      []config.Site
	Inventory  discovery.Registry
	Connection agent.Connection
	Reports    []model.Report
	Host       metrics.Sample
	Events     []storage.Record
	LoadedAt   time.Time
}
type Source struct {
	Live    bool
	Refresh chan struct{}
	Config  config.Config
	Store   *storage.Store
	Demo    bool
	MaxAge  time.Duration
}

func (s Source) Base(ctx context.Context) (Snapshot, error) {
	var inventory discovery.Registry
	if !s.Demo && s.Config.Discovering() {
		var err error
		inventory, err = s.Store.Inventory(ctx, s.Config.Host.ID)
		if err != nil {
			return Snapshot{}, err
		}
		s.Config = discovery.ConfigSites(inventory, s.Config)
	}
	reports, err := s.Store.Latest(ctx, s.Config.Host.ID)
	if err != nil {
		return Snapshot{}, err
	}
	byID := map[string]model.Report{}
	for _, r := range reports {
		byID[r.SiteID] = r
	}
	if len(reports) < len(s.Config.Sites) {
		external, err := s.Store.Query(ctx, s.Config.Host.ID, "", "external_health", time.Time{}, 1000)
		if err != nil {
			return Snapshot{}, err
		}
		for _, record := range external {
			if _, ok := byID[record.SiteID]; !ok {
				var r model.Report
				if json.Unmarshal(record.Data, &r) == nil {
					byID[r.SiteID] = r
				}
			}
		}
	}
	snapshot := Snapshot{Sites: s.Config.Sites, Inventory: inventory, LoadedAt: time.Now().UTC(), Reports: []model.Report{}, Host: metrics.Sample{HostID: s.Config.Host.ID, Values: map[string]float64{}, Unavailable: []string{}}}
	if data, err := s.Store.State(ctx, s.Config.Host.ID, "", "connection"); err != nil {
		return Snapshot{}, err
	} else if len(data) > 0 {
		if err = json.Unmarshal(data, &snapshot.Connection); err != nil {
			return Snapshot{}, err
		}
	}
	age := s.MaxAge
	if age == 0 {
		age = 3 * time.Minute
	}
	for _, site := range s.Config.Sites {
		r, ok := byID[site.ID]
		if !ok {
			r = model.Report{HostID: site.HostID, SiteID: site.ID, Name: site.Name, Overall: "unknown", Checks: []model.Check{{Layer: "collector", State: model.Unknown, Summary: "No observations yet"}}}
		} else if !s.Demo && time.Since(r.ObservedAt) > age {
			for i := range r.Checks {
				if r.Checks[i].State != model.Skip {
					r.Checks[i].State = model.Unknown
					r.Checks[i].Summary = "Stale observation: " + r.Checks[i].Summary
				}
			}
			r.Checks = append(r.Checks, model.Check{Layer: "collector", State: model.Unknown, Summary: "Observations are stale"})
			r.Overall = model.Overall(r.Checks)
		}
		if s.Live && !snapshot.Connection.Connected {
			for i := range r.Checks {
				if r.Checks[i].Layer == "runtime" || r.Checks[i].Layer == "container_health" || r.Checks[i].Layer == "proxy_route" {
					if r.Checks[i].State != model.Skip {
						r.Checks[i].State = model.Unknown
						r.Checks[i].Summary = "Collector unavailable: " + r.Checks[i].Summary
					}
				}
			}
			r.Checks = append(r.Checks, model.Check{Layer: "collector", State: model.Unknown, Summary: "Collector connection unavailable"})
			r.Overall = model.Overall(r.Checks)
		}
		snapshot.Reports = append(snapshot.Reports, r)
	}
	host, err := s.Store.Query(ctx, s.Config.Host.ID, "", "host_metrics", time.Time{}, 1)
	if err != nil {
		return Snapshot{}, err
	}
	if len(host) > 0 {
		if err = json.Unmarshal(host[0].Data, &snapshot.Host); err != nil {
			return Snapshot{}, err
		}
		if !s.Demo && time.Since(snapshot.Host.ObservedAt) > age {
			snapshot.Host.Values = map[string]float64{}
			snapshot.Host.Unavailable = append(snapshot.Host.Unavailable, "host observations are stale")
		}
	}
	if !s.Demo && s.Config.Discovering() {
		if data, err := s.Store.State(ctx, s.Config.Host.ID, "", "host_metrics"); err != nil {
			return Snapshot{}, err
		} else if len(data) > 0 {
			if err = json.Unmarshal(data, &snapshot.Host); err != nil {
				return Snapshot{}, err
			}
		}
		if time.Since(snapshot.Host.ObservedAt) > 5*time.Second {
			snapshot.Host.Values = map[string]float64{}
			snapshot.Host.Unavailable = append(snapshot.Host.Unavailable, "Telemetry is stale; collector connection may be unavailable")
		}
	}
	snapshot.Events, err = s.Store.Query(ctx, s.Config.Host.ID, "", "event", time.Now().Add(-24*time.Hour), 12)
	return snapshot, err
}

func (s Source) Page(ctx context.Context, page, siteID, target string, snapshot Snapshot) ([]string, error) {
	if snapshot.Sites != nil {
		s.Config.Sites = snapshot.Sites
	}
	runner := platform.Command{Host: s.Config.Host}
	sites, err := s.Config.Select(siteID)
	if err != nil {
		return nil, err
	}
	byID := map[string]model.Report{}
	for _, r := range snapshot.Reports {
		byID[r.SiteID] = r
	}
	switch page {
	case "inventory":
		lines := inventoryLines(snapshot.Inventory)
		if s.Live && !snapshot.Connection.Connected {
			lines = append([]string{"Collector unavailable; showing retained inventory", ""}, lines...)
		}
		return lines, nil
	case "overview", "sites", "issues":
		return nil, nil
	case "tools":
		return nil, nil
	case "site":
		if len(sites) == 0 {
			return []string{"Site is no longer in the active inventory."}, nil
		}
		return groupProperty(groupFor(s.Config.Sites, siteID), byID), nil
	case "logs":
		if s.Demo {
			return []string{"DEMO LOGS (fictional)", "2026-10-08 GET /healthz 200", "2026-10-08 application started", "api_key=[REDACTED]"}, nil
		}
		text, err := logs.Read(ctx, runner, sites[0])
		if err != nil {
			return nil, err
		}
		return append([]string{"Latest 200 lines; common secrets redacted. Review before sharing.", ""}, strings.Split(console.Safe(text), "\n")...), nil
	case "host":
		return console.Format("metrics", snapshot.Host), nil
	case "disk":
		if s.Demo {
			return console.Format("disk", DemoDisk()), nil
		}
		return console.Format("disk", metrics.Disk(ctx, runner, s.Config.Host)), nil
	case "readiness":
		out := []readiness.Assessment{}
		for _, site := range sites {
			r := byID[site.ID]
			if r.Runtime.Type == "external" {
				r.Runtime = model.Runtime{}
			}
			a, err := readiness.Assess(ctx, s.Config, site, r, s.Store)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return console.Format("readiness", out), nil
	case "recovery":
		out := []recovery.Assessment{}
		for _, site := range sites {
			a, err := recovery.Assess(ctx, s.Config, site, s.Store)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		return console.Format("recovery", out), nil
	case "costs":
		r, err := costs.Assess(ctx, s.Config, s.Store)
		return console.Format("costs", r), err
	case "capacity":
		r, err := capacity.Assess(ctx, s.Config, s.Store)
		return console.Format("capacity", r), err
	case "security":
		if s.Demo {
			return console.Format("security", DemoSecurity()), nil
		}
		r := security.Inspect(ctx, s.Config, snapshot.Reports, runner)
		return console.Format("security", r), nil
	case "preflight":
		if s.Demo {
			return console.Format("preflight", DemoPreflight(s.Config)), nil
		}
		r, err := preflight.Assess(ctx, s.Config, "reboot", snapshot.Reports, s.Store, runner)
		return console.Format("preflight", r), err
	case "impact":
		r, err := preflight.Dependency(s.Config, target)
		return console.Format("impact", r), err
	case "incidents":
		records, err := incidents.List(ctx, s.Store, s.Config.Host.ID, siteID)
		return console.Format("incidents", records), err
	case "help":
		return HelpLines(), nil
	default:
		kind := map[string]string{"events": "event", "changes": "change", "deployments": "deployment", "monitoring": "health", "external": "external_health"}[page]
		if kind == "" {
			return nil, fmt.Errorf("unknown dashboard view")
		}
		records, err := s.Store.Query(ctx, s.Config.Host.ID, siteID, kind, time.Now().Add(-24*time.Hour), 100)
		if err != nil {
			return nil, err
		}
		if page == "events" {
			for _, extra := range []string{"change", "deployment"} {
				more, err := s.Store.Query(ctx, s.Config.Host.ID, siteID, extra, time.Now().Add(-24*time.Hour), 100)
				if err != nil {
					return nil, err
				}
				records = append(records, more...)
			}
		}
		return timelineLines(page, records), nil
	}
}

func property(site config.Site, r model.Report) []string {
	lines := []string{console.Safe(site.Name) + "  [" + strings.ToUpper(r.Overall) + "]", "Site: " + site.ID + "  Host: " + site.HostID, "Observed: " + console.At(r.ObservedAt) + "  Source: " + r.Vantage, "", "PRODUCTION PATH: " + chain(r), ""}
	for _, c := range r.Checks {
		lines = append(lines, fmt.Sprintf("[%-7s] %-16s %s", strings.ToUpper(string(c.State)), c.Layer, console.Safe(c.Summary)))
	}
	lines = append(lines, "", "DOCTOR / EVIDENCE")
	if len(r.Diagnoses) == 0 {
		lines = append(lines, "No diagnosis from current observations.")
	}
	for _, d := range r.Diagnoses {
		lines = append(lines, d.Finding+" ("+d.Confidence+" confidence)")
		for _, e := range d.Evidence {
			lines = append(lines, "  Evidence: "+e)
		}
		lines = append(lines, "  Next: "+d.Investigation)
	}
	lines = append(lines, "", "SITE PASSPORT", "Repository: "+unknown(site.Repository), "Branch: "+unknown(site.Branch), "Domain: "+unknown(site.Proxy.Domain), "Upstream: "+unknown(site.Proxy.Upstream), "Runtime: "+site.Runtime.Type, "Image: "+unknown(r.Runtime.Image), "Revision: "+unknown(r.Runtime.Commit), "Restart policy: "+unknown(r.Runtime.RestartPolicy), "Public endpoint: "+unknown(site.Health.PublicURL), "", "[l] runtime logs   [a] site readiness   [b] site recovery   [Esc] all sites")
	for i := range lines {
		lines[i] = console.Safe(lines[i])
	}
	return lines
}
func unknown(s string) string {
	if s == "" {
		return "UNKNOWN"
	}
	return s
}
func chain(r model.Report) string {
	var b strings.Builder
	for _, layer := range []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"} {
		state := model.Skip
		found := false
		for _, c := range r.Checks {
			if c.Layer == layer {
				state = worseState(state, c.State)
				found = true
			}
		}
		if !found {
			state = model.Unknown
		}
		mark := map[model.State]string{model.Pass: "+", model.Fail: "!", model.Warn: "~", model.Unknown: "?", model.Skip: "-"}[state]
		b.WriteString("[" + mark + "] ")
	}
	return strings.TrimSpace(b.String())
}

func HelpLines() []string {
	return []string{
		"AMADEUS HOSTOPS - TERMINAL OPERATIONS DESK", "",
		"Menu: Up/Down or j/k previews a view; Right or Enter focuses content",
		"Content: Up/Down scrolls; on Sites, selects a property",
		"Left returns focus to the menu; Tab switches between panes",
		"Menu letters jump directly to a view, e.g. o, s, i, z",
		"a: optional Tools (costs, capacity, exposure and evidence assessments)",
		"Timeline combines transitions, infrastructure changes and deployments.",
		"Disk/security/preflight inspections start when entering content, not browsing.",
		"PageUp/PageDown, Home/End: move within the focused pane",
		"Enter on Sites: open property health, doctor and passport",
		"/ on Sites: filter by name, ID or domain; Esc clears the filter",
		"Space: refresh in the background (Disk scans on demand)",
		"F5: request inventory refresh and public checks",
		"z: inventory sources, routes, workloads and coverage gaps",
		"Esc: back from property; clear site filter or cancel input",
		"l from property: runtime logs; a: site readiness; b: recovery",
		"x: dependency impact; / then enter a resource, e.g. caddy",
		"w on Incidents: export the newest listed incident capsule",
		"t in DEMO: simulate an application outage / recovery",
		"q or Ctrl+C: quit and restore the terminal", "",
		"CPU/RAM 1s via direct kernel reads; workload metrics/inventory/disk 30s.",
		"Public HTTP uses --interval (default 30s); DNS/TLS use 5 minutes.",
		"Sites come from proxy domains; JSON site entries are optional metadata.",
		"Base/www domains share a row; the inspector keeps separate domain checks.",
		"The standalone collector is optional if this dashboard is running.",
		"No access token, browser or listening management port is used.",
		"SSH starts a bundled Linux collector in the remote user's private cache.",
		"Checks never restart, deploy, prune, remove files or reboot.",
		"DEMO is clearly labelled fictional data and makes no network calls.", "",
		"[+] passed  [!] failed  [~] warning  [?] unknown  [-] not applicable",
		"Layer order: runtime, local HTTP, proxy, DNS, TLS, public HTTP.",
		"UNKNOWN is missing evidence, not a zero measurement or a pass.",
		"Existing deployment workflows keep responsibility for deployment.",
		"Record evidence: hostops record deployment --file local/deploy.json",
	}
}
