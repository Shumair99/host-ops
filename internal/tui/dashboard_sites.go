package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/model"
)

type fleetColumn struct {
	label, layer string
	width        int
}

func attention(r model.Report) string {
	checks := append([]model.Check(nil), r.Checks...)
	sort.SliceStable(checks, func(i, j int) bool {
		return worseState(checks[i].State, checks[j].State) == checks[i].State && checks[i].State != checks[j].State
	})
	labels, seen := []string{}, map[string]bool{}
	for _, c := range checks {
		if c.State != model.Pass && c.State != model.Skip && !seen[c.Layer] {
			labels = append(labels, layerLabel(c.Layer))
			seen[c.Layer] = true
		}
	}
	if len(labels) == 0 {
		return "—"
	}
	return strings.Join(labels, ", ")
}

func fleetLayout(available int, problem bool) (int, []fleetColumn) {
	minimum := max(10, min(24, available/3))
	used := 9 // health and separator
	candidates := []fleetColumn{{"Public HTTP", "public_http", 11}, {"Runtime", "runtime", 8}}
	if problem {
		candidates = append(candidates, fleetColumn{"Attention", "attention", 13})
	}
	candidates = append(candidates, fleetColumn{"Latency", "latency", 8}, fleetColumn{"TLS", "tls", 5}, fleetColumn{"DNS", "dns", 5}, fleetColumn{"Proxy", "proxy_route", 6}, fleetColumn{"Local HTTP", "local_http", 10})
	cols := []fleetColumn{}
	for _, c := range candidates {
		if minimum+1+used+c.width+1 <= available {
			cols = append(cols, c)
			used += c.width + 1
		}
	}
	return max(8, min(32, available-used-1)), cols
}

func (m dashboard) fleetSummary() string {
	healthy, attentionCount, unknownCount := 0, 0, 0
	for _, s := range m.sites() {
		switch m.report(s.ID).Overall {
		case "healthy":
			healthy++
		case "critical", "warning":
			attentionCount++
		default:
			unknownCount++
		}
	}
	return fmt.Sprintf("%d properties  ", len(m.sites())) + m.theme.paint(fmt.Sprintf("%d healthy", healthy), mint) + "  " + m.theme.paint(fmt.Sprintf("%d attention", attentionCount), amber) + "  " + m.theme.paint(fmt.Sprintf("%d unknown", unknownCount), muted)
}

func (m dashboard) overviewSiteTable(w int) []string {
	t, available := m.theme, w-4
	problem := false
	for _, s := range m.sites() {
		problem = problem || attention(m.report(s.ID)) != "—"
	}
	nameWidth, cols := fleetLayout(available, problem)
	heading := pad("PROPERTY", nameWidth) + " " + pad("HEALTH", 8) + " "
	for _, c := range cols {
		heading += pad(c.label, c.width) + " "
	}
	rows := []string{clip(m.fleetSummary(), available), t.paint(clip(heading, available), muted)}
	for _, s := range m.sites() {
		r := m.report(s.ID)
		line := t.paint(pad(clean(s.Name), nameWidth), ink) + " " + t.paint(pad(strings.ToUpper(r.Overall), 8), statusColor(r.Overall)) + " "
		hasAttention := false
		for _, c := range cols {
			switch c.layer {
			case "attention":
				line += t.paint(pad(attention(r), c.width), statusColor(r.Overall)) + " "
				hasAttention = true
			case "latency":
				line += t.paint(pad(httpLatency(r), c.width), muted) + " "
			default:
				line += t.stateCell(r, c.layer, c.width) + " "
			}
		}
		rows = append(rows, clip(line, available))
		if !hasAttention && attention(r) != "—" {
			rows = append(rows, clip(t.paint("  Attention: "+attention(r), statusColor(r.Overall)), available))
		}
	}
	if len(m.sites()) == 0 {
		rows = append(rows, "No discovered properties yet")
	}
	return t.panel("Production properties", rows, w)
}

func runtimeAge(r model.Runtime) string {
	at, err := time.Parse(time.RFC3339Nano, r.StartedAt)
	if err != nil {
		return "—"
	}
	return durationLabel(max(0, time.Since(at).Seconds()))
}

func (m dashboard) siteDetails(s config.Site, w int, compact bool) []string {
	t, r := m.theme, m.report(s.ID)
	group := groupFor(m.rawSites(), s.ID)
	sample := m.snapshot.Workloads[s.ID]
	mem := "—"
	if n := value(sample.Values, "memory_used_bytes"); n != nil {
		mem = console.Bytes(*n)
	}
	rows := []string{t.badge(r.Overall) + t.paint("  report "+ageLabel(r.ObservedAt), muted)}
	if compact {
		rows = append(rows, clean(s.Proxy.Domain), "CPU "+percent(value(sample.Values, "cpu_percent"))+"  Memory "+mem+"  Uptime "+runtimeAge(r.Runtime), clip("Attention: "+attention(r), w-4))
	} else {
		for _, domain := range group.domains() {
			rows = append(rows, clean(domain.Proxy.Domain)+" "+t.badge(domainReport(domain, reportsByID(m.snapshot.Reports)).Overall))
		}
		for _, field := range [][2]string{{"Runtime", s.Runtime.Type + " / " + r.Runtime.Status}, {"CPU", percent(value(sample.Values, "cpu_percent"))}, {"Memory", mem}, {"Uptime", runtimeAge(r.Runtime)}, {"Upstream", unknown(s.Proxy.Upstream)}, {"Image", unknown(r.Runtime.Image)}, {"Revision", unknown(r.Runtime.Commit)}, {"Attention", attention(r)}} {
			rows = append(rows, clip(t.paint(pad(field[0], 10), muted)+" "+clean(field[1]), w-4))
		}
		rows = append(rows, t.paint("Workload sample "+ageLabel(sample.ObservedAt), muted))
	}
	return t.panel("Selected property", rows, w)
}

func (m dashboard) sitesWorkspace(w int) []string {
	sites := m.filteredSites()
	if len(sites) == 0 {
		return m.theme.panel("Workloads", []string{"No matching properties"}, w)
	}
	selected := min(m.selected, len(sites)-1)
	split := w >= 88
	listWidth := w
	if split {
		listWidth = max(38, (w-1)/2)
	}
	details := m.siteDetails(sites[selected], w, true)
	limit := max(1, m.bodyHeight()-len(details)-6)
	if split {
		details = m.siteDetails(sites[selected], w-listWidth-1, false)
		limit = max(1, m.bodyHeight()-5)
	}
	start := max(0, selected-limit+1)
	end := min(len(sites), start+limit)
	inside := listWidth - 4
	nameWidth := max(8, min(32, inside-20))
	rows := []string{m.theme.paint(fmt.Sprintf("%d properties · %d–%d", len(sites), start+1, end), muted), m.theme.paint(pad("PROPERTY", nameWidth)+" "+pad("HEALTH", 8)+" RUNTIME", muted)}
	if m.filter != "" {
		rows[0] = clip(m.theme.paint("Filter: "+clean(m.filter), cyan), inside)
	}
	for i := start; i < end; i++ {
		s, r := sites[i], m.report(sites[i].ID)
		line := m.theme.paint(pad(clean(s.Name), nameWidth), ink) + " " + m.theme.paint(pad(strings.ToUpper(r.Overall), 8), statusColor(r.Overall)) + " " + clean(s.Runtime.Type)
		if i == selected {
			line = m.theme.selection(line, inside)
		}
		rows = append(rows, clip(line, inside))
	}
	list := m.theme.panel("Workloads", rows, listWidth)
	if split {
		return columns(list, details, listWidth, w-listWidth-1)
	}
	return append(append(list, ""), details...)
}

func (m dashboard) systemSummary(w int) []string {
	v := m.snapshot.Host.Values
	uptime, load, swap := "—", "—", "—"
	if n := value(v, "uptime_seconds"); n != nil {
		uptime = durationLabel(*n)
	}
	if n := value(v, "load_1"); n != nil {
		load = fmt.Sprintf("%.2f", *n)
	}
	if n := value(v, "swap_used_bytes"); n != nil {
		swap = console.Bytes(*n)
	}
	workloads := "—"
	if !m.snapshot.Inventory.Snapshot.ObservedAt.IsZero() {
		workloads = fmt.Sprint(len(m.snapshot.Inventory.Snapshot.Workloads))
	}
	return []string{clip(m.theme.strong("SYSTEM", cyan)+m.theme.paint("  Uptime "+uptime+"  Load "+load+"  Swap "+swap+"  Workloads "+workloads, muted), w)}
}
