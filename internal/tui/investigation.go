package tui

import (
	"fmt"
	"sort"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
)

var toolExplanations = map[string][]string{
	"security":    {"Container hardening, published ports and listening sockets.", "Warnings suggest improvements; unknown means unobserved."},
	"capacity":    {"Measured percentiles and available disk space.", "Storage forecasts appear when sufficient history exists."},
	"impact":      {"Inspect the properties sharing a dependency.", "Use / to enter a host, proxy or workload."},
	"readiness":   {"Evidence coverage across applicable production checks.", "Provisional means evidence is missing; this is not uptime."},
	"recovery":    {"Reported backups, restore tests and data-loss/time targets.", "Unknown means no report; backup files are not auto-detected."},
	"preflight":   {"Check restart, health, capacity and recovery before reboot.", "Read-only assessment; it does not reboot the host."},
	"changes":     {"Review infrastructure changes in a dated evidence feed.", "Includes an activity distribution over the fetched records."},
	"deployments": {"Review pipeline outcomes, revisions and health results.", "Import records through hostops record deployment."},
	"external":    {"Inspect observations from an independent watcher.", "A time-cell map keeps unobserved periods visible."},
	"costs":       {"Compare monthly estimates and allocation shares.", "Enter opens billing settings here."},
}

func (m dashboard) inspectorSummary(s config.Site, w int) []string {
	t, r := m.theme, m.report(s.ID)
	runtimeSite := propertyRuntimeSite(groupFor(m.rawSites(), s.ID).domains(), s)
	runtimeReport := domainReport(runtimeSite, reportsByID(m.snapshot.Reports))
	rows := []string{t.badge(r.Overall) + "  " + clean(runtimeSite.Runtime.Type) + " / " + clean(runtimeReport.Runtime.Status) + "  " + t.paint("HTTP "+httpLatency(r), muted)}
	if finding := primaryFinding(r); finding != "" {
		rows = append(rows, wrap([]string{t.strong(finding, amber)}, w-4)...)
	}
	rows = append(rows, t.paint("ROUTE", muted)+"  "+clean(runtimeSite.Proxy.Domain)+" → "+clean(unknown(runtimeSite.Proxy.Upstream)))
	if runtimeSite.ID != s.ID {
		rows = append(rows, t.paint("REDIRECT", muted)+"  "+clean(s.Proxy.Domain)+" → "+clean(runtimeSite.Proxy.Domain))
	}
	rows = append(rows, t.paint("RUNTIME", muted)+"  "+clean(unknown(runtimeReport.Runtime.Name))+" · "+clean(unknown(runtimeReport.Runtime.Image)))
	rows = append(rows, t.paint("REVISION", muted)+" "+clean(unknown(runtimeReport.Runtime.Commit))+fmt.Sprintf(" · restarts %d", runtimeReport.Runtime.RestartCount))
	out := t.panel("SITE PASSPORT / operating context", rows, w)
	if len(m.snapshot.SiteHistory) > 0 && m.snapshot.SiteHistory[0].SiteID == runtimeSite.ID {
		sample := m.snapshot.Workloads[runtimeSite.ID]
		if w >= 76 {
			a := (w - 1) / 2
			out = append(out, columns(m.signalPanel("Workload CPU / 30m", "cpu_percent", "%", cyan, 0, m.snapshot.SiteHistory, value(sample.Values, "cpu_percent"), a, 3), m.signalPanel("Workload memory / 30m", "memory_used_bytes", "B", violet, 0, m.snapshot.SiteHistory, value(sample.Values, "memory_used_bytes"), w-a-1, 3), a, w-a-1)...)
		} else {
			rows := []string{"CPU " + percent(value(sample.Values, "cpu_percent")) + " · memory " + signalValue(value(sample.Values, "memory_used_bytes"), "B"), t.paint("Measured workload history · expand terminal for plots", muted)}
			out = append(out, t.panel("Workload resources", rows, w)...)
		}
	}
	return out
}

func (m dashboard) discoveryTopology(w int) []string {
	t, r := m.theme, m.snapshot.Inventory
	rows := []string{t.strong(fmt.Sprintf("%d domains", len(r.Sites)), ink) + " → " + t.strong(fmt.Sprintf("%d routes", len(r.Snapshot.Routes)), ink) + " → " + t.strong(fmt.Sprintf("%d workloads", len(r.Snapshot.Workloads)), ink)}
	linked := map[string]bool{}
	for _, site := range r.Sites {
		for _, id := range site.WorkloadIDs {
			linked[id] = true
		}
	}
	unmapped := 0
	types := map[string]int{}
	running := 0
	for _, v := range r.Snapshot.Workloads {
		types[v.Type]++
		if !linked[v.ID] {
			unmapped++
		}
		if v.Status == "running" || v.Status == "active" {
			running++
		}
	}
	rows = append(rows, t.paint(fmt.Sprintf("%d running · %d not linked to domain routes · %d listeners", running, unmapped, len(r.Snapshot.Listeners)), muted))
	if m.source.Live && !m.snapshot.Connection.Connected {
		rows = append(rows, t.paint("Collector unavailable · retained inventory", amber))
	}
	keys := []string{}
	for key := range types {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		n := float64(types[key]) / float64(max(1, len(r.Snapshot.Workloads))) * 100
		rows = append(rows, t.paint(pad(clean(key), 10), muted)+t.meter(&n, max(1, w-22), cyan)+fmt.Sprintf(" %d", types[key]))
	}
	if r.Snapshot.ObservedAt.IsZero() {
		rows = append(rows, t.paint("Discovery inventory has not been observed", muted))
	}
	return t.panel("Discovery topology", rows, w)
}

func (m dashboard) storageSignals(w int) []string {
	disk := m.snapshot.Disk
	if disk == nil {
		return nil
	}
	t := m.theme
	rows := []string{}
	for _, fs := range disk.Filesystems {
		if fs.Mount != "/" {
			continue
		}
		p := 0.0
		if fs.Inodes > 0 {
			p = float64(fs.InodesUsed) / float64(fs.Inodes) * 100
			rows = append(rows, t.paint("Inodes  ", muted)+t.meter(&p, max(1, w-24), metricColor(&p, cyan))+fmt.Sprintf(" %.1f%%", p))
		}
		rows = append(rows, t.paint("Available  ", muted)+console.Bytes(float64(fs.AvailableBytes)))
	}
	if len(rows) == 0 {
		return nil
	}
	return t.panel("Storage pressure", rows, w)
}
