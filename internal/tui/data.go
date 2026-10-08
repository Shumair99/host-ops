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
	Sites         []config.Site
	Inventory     discovery.Registry
	Connection    agent.Connection
	Reports       []model.Report
	Host          metrics.Sample
	History       []metrics.Sample
	Workloads     map[string]metrics.Sample
	Disk          *metrics.DiskReport
	DiskScan      agent.DiskScan
	Detail        any
	Events        []storage.Record
	LoadedAt      time.Time
	HistoryNotice string
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
	if data, err := s.Store.State(ctx, "hostops", "", "history_pressure"); err != nil {
		return Snapshot{}, err
	} else if len(data) > 0 {
		var notice struct {
			Issue string `json:"issue"`
		}
		if json.Unmarshal(data, &notice) == nil {
			snapshot.HistoryNotice = notice.Issue
		}
	}
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
		} else if !s.Demo {
			r = model.Current(r, time.Now(), age)
		}
		if s.Live && !snapshot.Connection.Connected {
			for i := range r.Checks {
				if r.Checks[i].Layer == "runtime" || r.Checks[i].Layer == "container_health" || r.Checks[i].Layer == "proxy_route" || r.Checks[i].Layer == "proxy" {
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
	host, err := s.Store.Query(ctx, s.Config.Host.ID, "", "host_metrics", time.Time{}, 120)
	if err != nil {
		return Snapshot{}, err
	}
	if len(host) > 0 {
		for i := len(host) - 1; i >= 0; i-- {
			var sample metrics.Sample
			if json.Unmarshal(host[i].Data, &sample) == nil {
				snapshot.History = append(snapshot.History, sample)
			}
		}
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
	snapshot.Workloads = map[string]metrics.Sample{}
	workloads, err := s.Store.Query(ctx, s.Config.Host.ID, "", "metrics", time.Time{}, 200)
	if err != nil {
		return Snapshot{}, err
	}
	for _, record := range workloads {
		if _, exists := snapshot.Workloads[record.SiteID]; exists {
			continue
		}
		var sample metrics.Sample
		if json.Unmarshal(record.Data, &sample) == nil {
			if !s.Demo && time.Since(sample.ObservedAt) > age {
				sample.Values = nil
			}
			snapshot.Workloads[record.SiteID] = sample
		}
	}
	snapshot.Disk, snapshot.DiskScan, err = agent.DiskState(ctx, s.Store, s.Config.Host.ID)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Events, err = s.activity(ctx, "", 12)
	return snapshot, err
}

func (s Source) Page(ctx context.Context, page, siteID, target string, snapshot Snapshot) ([]string, error) {
	return s.Evidence(ctx, page, siteID, target, &snapshot)
}

// Evidence retains typed reports for the terminal widgets while Page supplies
// the existing plain text command presentation.
func (s Source) Evidence(ctx context.Context, page, siteID, target string, snapshot *Snapshot) ([]string, error) {
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
		return strings.Split(console.Safe(text), "\n"), nil
	case "host":
		return console.Format("metrics", snapshot.Host), nil
	case "disk":
		if s.Demo {
			disk := DemoDisk()
			snapshot.Disk = &disk
			return console.Format("disk", disk), nil
		}
		return nil, nil // The dedicated background inspection owns scanning.
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
		snapshot.Detail = out
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
		snapshot.Detail = out
		return console.Format("recovery", out), nil
	case "costs":
		r, err := costs.Assess(ctx, s.Config, s.Store)
		snapshot.Detail = r
		return console.Format("costs", r), err
	case "capacity":
		r, err := capacity.Assess(ctx, s.Config, s.Store)
		snapshot.Detail = r
		return console.Format("capacity", r), err
	case "security":
		if s.Demo {
			r := DemoSecurity()
			snapshot.Detail = r
			return console.Format("security", r), nil
		}
		r := security.Inspect(ctx, s.Config, snapshot.Reports, runner)
		snapshot.Detail = r
		return console.Format("security", r), nil
	case "preflight":
		if s.Demo {
			r := DemoPreflight(s.Config)
			snapshot.Detail = r
			return console.Format("preflight", r), nil
		}
		r, err := preflight.Assess(ctx, s.Config, "reboot", snapshot.Reports, s.Store, runner)
		snapshot.Detail = r
		return console.Format("preflight", r), err
	case "impact":
		r, err := preflight.Dependency(s.Config, target)
		snapshot.Detail = r
		return console.Format("impact", r), err
	case "incidents":
		records, err := incidents.List(ctx, s.Store, s.Config.Host.ID, siteID)
		snapshot.Detail = records
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
			records, err = s.activity(ctx, siteID, 100)
			if err != nil {
				return nil, err
			}
		}
		return timelineLines(page, records, timelineLabels(s.Config.Sites)), nil
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
		"Esc: back from property or optional tool; clear site filter or cancel input",
		"l from property: runtime logs; a: site readiness; b: recovery",
		"x: dependency impact; / then enter a resource, e.g. caddy",
		"w on Incidents: export the newest listed incident capsule",
		"t in DEMO: simulate an application outage / recovery",
		"q or Ctrl+C: quit and restore the terminal", "",
		"CPU/RAM 1s via direct kernel reads; workload metrics/inventory/disk capacity 30s.",
		"Public HTTP uses --interval (default 30s); DNS/TLS use 5 minutes.",
		"Sites come from proxy domains; JSON site entries are optional metadata.",
		"Base/www domains share a row; the inspector keeps separate domain checks.",
		"The standalone collector is optional if this dashboard is running.",
		"No access token, browser or listening management port is used.",
		"SSH starts a bundled Linux collector in the remote user's private cache.",
		"Checks never restart, deploy, prune, remove files or reboot.",
		"DEMO is clearly labelled fictional data and makes no network calls.", "",
		"HEALTH: green healthy, amber warning, red critical; words also carry state.",
		"OK passed; FAIL failed; WARN warning; UNKNOWN missing evidence; N/A inapplicable.",
		"Fleet columns expand with terminal width; all six checks remain in the inspector.",
		"Meters use measured 0-100% utilization; a dash means no measurement.",
		"Overview trends show the last 30 minutes on a fixed 0-100% scale.",
		"Blank Overview columns mean no sample was recorded; they are not zero.",
		"Paused collection or a disconnected SSH session can leave a gap in history.",
		"Host charts retain actual timestamps and their longer history window.",
		"Property metrics cover associated workloads; shared backends are not unique usage.",
		"Property CPU may exceed 100% when workloads use several cores.",
		"Directory bars compare sizes to the largest row, not summed filesystem use.",
		"Directory rows may overlap; >= totals are lower bounds from partial scans.",
		"Saved Disk accounting remains available across dashboard restarts.",
		"Storage inspection starts automatically in the background; cached results refresh after 30m.",
		"Directory walks use lower CPU priority, a 45s limit, depth 8 and a 4096-directory index.",
		"Disk: arrows select directories, Enter/Right opens, Backspace goes up, PgDn scrolls.",
		"Space on Disk requests a new scan; one scan per host/history database runs at a time.",
		"File contents are not read by storage scans; paths, sizes and ownership are stored locally.",
		"Shared property storage is not additive; writable sizes exclude images, mounts and logs.",
		"--no-collect or host.disk_scan_disabled disables automatic storage scans.",
		"Color: --color auto|always|never; NO_COLOR disables auto color.",
		"Windows Terminal with PowerShell is recommended for block and box glyphs.",
		"Optional billing: hostops billing list --provider hetzner (or hostinger)",
		"Connect budget: hostops billing set --provider NAME --resource ID",
		"Manual: hostops billing set --provider manual --monthly AMOUNT --currency GBP",
		"Own hardware: hostops billing set --provider self-hosted (budget optional)",
		"Cost allocation is an estimate over latest 1000 samples per property in 24h.",
		"Missing aggregate usage uses equal resource shares; unmanaged services are shared overhead.",
		"Backups/restores are operator-reported evidence; HostOps does not verify artifacts.",
		"Impact covers observed/configured dependencies; hidden dependencies may add sites.",
		"Logs show the latest 200 lines with common secret patterns redacted.",
		"UNKNOWN is missing evidence, not a zero measurement or a pass.",
		"Existing deployment workflows keep responsibility for deployment.",
		"Record evidence: hostops record deployment --file local/deploy.json",
	}
}
