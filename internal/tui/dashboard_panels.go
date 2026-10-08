package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
)

func ageLabel(at time.Time) string {
	if at.IsZero() {
		return "—"
	}
	d := max(time.Duration(0), time.Since(at))
	if d < time.Second {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}
func durationLabel(seconds float64) string {
	d := time.Duration(seconds) * time.Second
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	return d.Round(time.Minute).String()
}
func (m dashboard) overviewLines(w int) []string {
	out := m.resourcePanels(w)
	out = append(out, "")
	out = append(out, m.siteTable(w, false)...)
	events := []string{}
	for _, r := range m.snapshot.Events[:min(4, len(m.snapshot.Events))] {
		label := clean(r.SiteID)
		if label == "" {
			label = "host"
		}
		events = append(events, m.theme.paint(r.ObservedAt.Local().Format("15:04:05"), muted)+"  "+m.theme.paint(label, cyan)+"  "+m.theme.paint(recordSummary(r.Data), ink))
	}
	if len(events) == 0 {
		events = []string{m.theme.paint("No recorded changes in 24h", muted)}
	}
	out = append(out, "")
	return append(out, m.theme.panel("Recent activity", events, w)...)
}
func (m dashboard) resourcePanels(w int) []string {
	t := m.theme
	specs := []struct {
		label, key string
		color      int
	}{{"CPU", "cpu_percent", cyan}, {"Memory", "memory_percent", violet}, {"Root disk", "disk_percent", amber}}
	if w < 76 {
		rows := []string{}
		for _, s := range specs {
			n := value(m.snapshot.Host.Values, s.key)
			color := metricColor(n, s.color)
			rows = append(rows, t.paint(pad(s.label, 10), muted)+" "+t.meter(n, max(4, w-26), color)+" "+t.strong(pad(percent(n), 7), color))
		}
		if n := value(m.snapshot.Host.Values, "disk_free_bytes"); n != nil {
			rows = append(rows, t.paint("Disk available", muted)+"  "+t.strong(console.Bytes(*n), amber))
		}
		return t.panel("Host resources", rows, w)
	}
	sizes := []int{(w - 2) / 3, (w - 2) / 3, w - 2 - 2*((w-2)/3)}
	cards := [][]string{}
	for i, s := range specs {
		n := value(m.snapshot.Host.Values, s.key)
		c := metricColor(n, s.color)
		inside := sizes[i] - 4
		detail := "Host utilization"
		if s.key == "memory_percent" {
			detail = m.bytePair("memory_used_bytes", "memory_total_bytes")
		}
		if s.key == "disk_percent" {
			detail = "Available " + m.byteValue("disk_free_bytes")
		}
		rows := []string{t.strong(percent(n), c) + "  " + t.paint("used", muted), t.meter(n, inside, c), t.paint(detail, muted)}
		rows = append(rows, m.spark(s.key, inside, c)...)
		rows = append(rows, t.paint("Trend / relative scale", muted))
		cards = append(cards, t.panel(s.label, rows, sizes[i]))
	}
	return columns(columns(cards[0], cards[1], sizes[0], sizes[1]), cards[2], sizes[0]+sizes[1]+1, sizes[2])
}
func (m dashboard) byteValue(key string) string {
	if n := value(m.snapshot.Host.Values, key); n != nil {
		return console.Bytes(*n)
	}
	return "—"
}
func (m dashboard) bytePair(a, b string) string { return m.byteValue(a) + " / " + m.byteValue(b) }
func (m dashboard) spark(key string, width, color int) []string {
	samples := m.snapshot.History
	if len(samples) < 2 {
		return []string{m.theme.paint("Awaiting history", muted), ""}
	}
	buckets := timeBuckets(samples, key, width)
	peak := 0.0
	count := 0
	for _, v := range buckets {
		if v != nil {
			peak = max(peak, *v)
			count++
		}
	}
	if count == 0 {
		return []string{m.theme.paint("No measured values", muted), ""}
	}
	rows := barPlot(buckets, 2, peak)
	for i := range rows {
		rows[i] = m.theme.paint(rows[i], color)
	}
	return rows
}

// Full and half blocks are supported by Consolas, Lucida Console and Cascadia
// Mono. Stacking them avoids the fractional glyphs absent in classic consoles.
// A measured zero has an ASCII baseline; a missing observation remains blank.
func barPlot(buckets []*float64, height int, ceiling float64) []string {
	height = max(1, height)
	rows := make([]string, height)
	for row := range rows {
		y := height - row - 1
		var b strings.Builder
		for _, v := range buckets {
			glyph := ' '
			if v != nil {
				level := 0
				if *v > 0 && ceiling > 0 {
					level = max(1, int(math.Round(math.Min(1, *v/ceiling)*float64(height*2))))
				}
				switch {
				case level >= 2*y+2:
					glyph = '█'
				case level > 2*y:
					glyph = '▄'
				case level == 0 && y == 0:
					glyph = '_'
				}
			}
			b.WriteRune(glyph)
		}
		rows[row] = b.String()
	}
	return rows
}

// Timestamp buckets preserve actual spacing and holes in the retained history.
// No interpolation or synthetic points are used, and the right edge is newest.
func timeBuckets(samples []metrics.Sample, key string, width int) []*float64 {
	out := make([]*float64, max(1, width))
	if len(samples) == 0 {
		return out
	}
	first, last := samples[0].ObservedAt, samples[len(samples)-1].ObservedAt
	span := last.Sub(first).Seconds()
	for _, s := range samples {
		n := value(s.Values, key)
		if n == nil {
			continue
		}
		x := len(out) - 1
		if span > 0 {
			x = int(math.Round(s.ObservedAt.Sub(first).Seconds() / span * float64(len(out)-1)))
		}
		if x >= 0 && x < len(out) {
			out[x] = n
		}
	}
	return out
}
func (m dashboard) trendPanel(label, key string, color, w int) []string {
	samples := m.snapshot.History
	if len(samples) < 2 {
		return m.theme.panel(label, []string{m.theme.paint("Awaiting history", muted)}, w)
	}
	plotWidth := max(1, w-11)
	buckets := timeBuckets(samples, key, plotWidth)
	peak := 0.0
	count := 0
	for _, v := range buckets {
		if v != nil {
			peak = max(peak, *v)
			count++
		}
	}
	if count == 0 {
		return m.theme.panel(label, []string{m.theme.paint("No measured values", muted)}, w)
	}
	ceiling := max(1.0, math.Ceil(peak/10)*10)
	rows := []string{}
	for row, plot := range barPlot(buckets, 4, ceiling) {
		axis := "     "
		if row == 0 {
			axis = fmt.Sprintf("%4.0f%%", ceiling)
		} else if row == 3 {
			axis = "   0%"
		}
		rows = append(rows, m.theme.paint(axis+" │", muted)+m.theme.paint(plot, color))
	}
	first, last := samples[0].ObservedAt, samples[len(samples)-1].ObservedAt
	rows = append(rows, m.theme.paint("       "+first.Local().Format("15:04")+strings.Repeat(" ", max(1, plotWidth-10))+last.Local().Format("15:04"), muted))
	return m.theme.panel(label, rows, w)
}
func (m dashboard) hostLines(w int) []string {
	out := m.resourcePanels(w)
	out = append(out, "")
	if w >= 76 {
		a := (w - 1) / 2
		out = append(out, columns(m.trendPanel("CPU history / %", "cpu_percent", cyan, a), m.trendPanel("Memory history / %", "memory_percent", violet, w-a-1), a, w-a-1)...)
	} else {
		out = append(out, m.trendPanel("CPU history / %", "cpu_percent", cyan, w)...)
		out = append(out, m.trendPanel("Memory history / %", "memory_percent", violet, w)...)
	}
	uptime := "—"
	if n := value(m.snapshot.Host.Values, "uptime_seconds"); n != nil {
		uptime = durationLabel(*n)
	}
	rows := []string{m.theme.paint("Uptime", muted) + "  " + uptime, "Memory  " + m.bytePair("memory_used_bytes", "memory_total_bytes"), "Storage " + m.bytePair("disk_used_bytes", "disk_total_bytes")}
	known := map[string]bool{"cpu_percent": true, "memory_percent": true, "disk_percent": true, "uptime_seconds": true, "memory_used_bytes": true, "memory_total_bytes": true, "disk_used_bytes": true, "disk_total_bytes": true, "disk_free_bytes": true, "disk_observed_unix_seconds": true}
	keys := []string{}
	for key := range m.snapshot.Host.Values {
		if !known[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		n := m.snapshot.Host.Values[key]
		v := fmt.Sprintf("%.2f", n)
		if strings.HasSuffix(key, "bytes") {
			v = console.Bytes(n)
		}
		rows = append(rows, pad(clean(strings.ReplaceAll(key, "_", " ")), 24)+" "+m.theme.paint(v, cyan))
	}
	for _, issue := range m.snapshot.Host.Unavailable {
		rows = append(rows, m.theme.paint("Unavailable · "+clean(issue), amber))
	}
	out = append(out, "")
	out = append(out, m.theme.panel("System", rows, w)...)
	return append(out, m.workloadPanel(w)...)
}
func layerState(r model.Report, layer string) model.State {
	state := model.Skip
	found := false
	for _, c := range r.Checks {
		if c.Layer == layer {
			state = worseState(state, c.State)
			found = true
		}
	}
	if !found {
		return model.Unknown
	}
	return state
}
func (t theme) stateCell(r model.Report, layer string, width int) string {
	state := layerState(r, layer)
	label := map[model.State]string{model.Pass: "OK", model.Fail: "FAIL", model.Warn: "WARN", model.Unknown: "UNKNOWN", model.Skip: "N/A"}[state]
	return t.paint(pad(label, width), statusColor(string(state)))
}
func (m dashboard) siteTable(w int, selectable bool) []string {
	t := m.theme
	sites := m.sites()
	if selectable {
		sites = m.filteredSites()
	}
	healthy, attention, unknownCount := 0, 0, 0
	for _, s := range m.sites() {
		switch m.report(s.ID).Overall {
		case "healthy":
			healthy++
		case "critical", "warning":
			attention++
		default:
			unknownCount++
		}
	}
	rows := []string{t.strong(fmt.Sprintf("%d", len(m.sites())), ink) + t.paint(" properties  ", muted) + t.paint(fmt.Sprintf("%d healthy", healthy), mint) + "  " + t.paint(fmt.Sprintf("%d attention", attention), amber) + "  " + t.paint(fmt.Sprintf("%d unknown", unknownCount), muted)}
	if selectable && m.filter != "" {
		rows = append(rows, t.paint("Filter  "+clean(m.filter), cyan))
	}
	type column struct {
		label, layer string
		width        int
	}
	cols := []column{{"Runtime", "runtime", 8}, {"Public HTTP", "public_http", 11}}
	if w < 48 {
		cols = []column{{"Public HTTP", "public_http", 11}}
	}
	if w >= 96 {
		cols = []column{{"Runtime", "runtime", 8}, {"Local HTTP", "local_http", 11}, {"Proxy", "proxy_route", 6}, {"DNS", "dns", 5}, {"TLS", "tls", 5}, {"Public HTTP", "public_http", 11}}
	}
	available := w - 4
	extra := 0
	for _, c := range cols {
		extra += c.width + 1
	}
	latency := w >= 78
	if latency {
		extra += 8
	}
	nameWidth := max(8, available-extra-12)
	heading := pad("PROPERTY", nameWidth) + " " + pad("HEALTH", 10) + " "
	for _, c := range cols {
		heading += pad(c.label, c.width) + " "
	}
	if latency {
		heading += "LATENCY"
	}
	rows = append(rows, t.paint(clip(heading, available), muted))
	for i, s := range sites {
		r := m.report(s.ID)
		line := t.paint(pad(clean(s.Name), nameWidth), ink) + " " + t.paint(pad(strings.ToUpper(r.Overall), 10), statusColor(r.Overall)) + " "
		for _, c := range cols {
			line += t.stateCell(r, c.layer, c.width) + " "
		}
		if latency {
			line += t.paint(httpLatency(r), muted)
		}
		if selectable && i == m.selected && !m.menuFocus {
			line = t.selection(line, available)
		}
		rows = append(rows, clip(line, available))
	}
	if len(sites) == 0 {
		rows = append(rows, t.paint("No matching properties", muted))
	}
	return t.panel("Production properties", rows, w)
}
func httpLatency(r model.Report) string {
	n := 0.0
	found := false
	for _, c := range r.Checks {
		if c.Layer == "public_http" && c.LatencyMS > 0 {
			n = max(n, c.LatencyMS)
			found = true
		}
	}
	if !found {
		return "—"
	}
	return fmt.Sprintf("%.0f ms", n)
}

var checkLabels = map[string]string{"runtime": "Runtime", "local_http": "Local HTTP", "proxy_route": "Proxy route", "dns": "DNS", "tls": "TLS", "public_http": "Public HTTP", "container_health": "Container health", "collector": "Collector"}

func layerLabel(key string) string {
	if label, ok := checkLabels[key]; ok {
		return label
	}
	return clean(strings.ReplaceAll(key, "_", " "))
}
func (m dashboard) checkRows(checks []model.Check, w int) []string {
	rows := []string{}
	for _, c := range checks {
		title := pad(layerLabel(c.Layer), 16) + " " + m.theme.badge(string(c.State))
		if c.StatusCode > 0 && (c.Layer == "local_http" || c.Layer == "public_http") {
			title += fmt.Sprintf("  HTTP %d", c.StatusCode)
		}
		if c.LatencyMS > 0 {
			title += m.theme.paint(fmt.Sprintf("  %.0f ms", c.LatencyMS), muted)
		}
		rows = append(rows, title)
		if c.State != model.Pass && c.State != model.Skip {
			rows = append(rows, wrap([]string{m.theme.paint(clean(c.Summary), statusColor(string(c.State)))}, w-4)...)
		}
	}
	return rows
}
func (m dashboard) inspectorLines(w int) []string {
	group := groupFor(m.rawSites(), m.siteID)
	byID := reportsByID(m.snapshot.Reports)
	if group.Primary.ID == "" {
		return m.theme.panel("Property", []string{"No active property selected"}, w)
	}
	out := []string{}
	for _, s := range group.domains() {
		r := domainReport(s, byID)
		rows := []string{m.theme.badge(r.Overall) + m.theme.paint("  "+ageLabel(r.ObservedAt), muted)}
		rows = append(rows, m.checkRows(r.Checks, w)...)
		rows = append(rows, m.theme.paint("Endpoint  "+clean(unknown(s.Health.PublicURL)), muted))
		out = append(out, m.theme.panel(clean(s.Proxy.Domain), rows, w)...)
		out = append(out, "")
		if len(r.Diagnoses) > 0 {
			diagnoses := []string{}
			for _, d := range r.Diagnoses {
				diagnoses = append(diagnoses, wrap([]string{m.theme.strong(clean(d.Finding), amber)}, w-4)...)
				for _, e := range d.Evidence {
					diagnoses = append(diagnoses, wrap([]string{clean(e)}, w-4)...)
				}
				diagnoses = append(diagnoses, wrap([]string{m.theme.paint("Next  "+clean(d.Investigation), muted)}, w-4)...)
			}
			out = append(out, m.theme.panel("Diagnosis", diagnoses, w)...)
		}
	}
	r := domainReport(group.Primary, byID)
	s := group.Primary
	fields := [][2]string{{"Runtime", s.Runtime.Type + " / " + r.Runtime.Status}, {"Upstream", s.Proxy.Upstream}, {"Image", r.Runtime.Image}, {"Revision", r.Runtime.Commit}, {"Restart policy", r.Runtime.RestartPolicy}, {"Repository", s.Repository}, {"Branch", s.Branch}}
	rows := []string{}
	for _, f := range fields {
		v := clean(f[1])
		if v == "" {
			v = "—"
		}
		rows = append(rows, wrap([]string{m.theme.paint(pad(f[0], 15), muted) + " " + v}, w-4)...)
	}
	return append(out, m.theme.panel("SITE PASSPORT", rows, w)...)
}
func (m dashboard) diskLines(w int) []string {
	t := m.theme
	n := value(m.snapshot.Host.Values, "disk_percent")
	rows := []string{t.meter(n, max(1, w-14), metricColor(n, cyan)) + " " + t.strong(percent(n), metricColor(n, cyan)), "Used " + m.byteValue("disk_used_bytes") + "   Free " + m.byteValue("disk_free_bytes"), t.paint("Capacity "+m.byteValue("disk_total_bytes"), muted)}
	out := t.panel("Root filesystem", rows, w)
	disk := m.snapshot.Disk
	if disk == nil {
		// Read failures still have their evidence, rather than disappearing behind an empty chart.
		if m.notice != "" && len(m.lines) > 0 {
			return append(out, t.panel("Inspection", m.decorate(m.lines), w)...)
		}
		return append(out, t.panel("Directory sizes", []string{t.paint("No inspection recorded", muted)}, w)...)
	}
	rows = []string{}
	largest := float64(0)
	for _, e := range disk.Directories {
		largest = max(largest, float64(e.Bytes))
	}
	for _, e := range disk.Directories {
		amount := console.Bytes(float64(e.Bytes))
		if e.Partial {
			amount = "≥ " + amount
		}
		pathWidth := max(10, (w-4)/2)
		barWidth := max(3, w-4-pathWidth-14)
		ratio := 0.0
		if largest > 0 {
			ratio = float64(e.Bytes) / largest * 100
		}
		rows = append(rows, t.paint(pad(clean(e.Path), pathWidth), ink)+" "+t.meter(&ratio, barWidth, violet)+" "+t.paint(amount, violet))
	}
	if len(rows) == 0 {
		rows = append(rows, t.paint("No directory measurements", muted))
	}
	if len(disk.Unavailable) > 0 {
		rows = append(rows, t.paint("Partial inspection · some totals are lower bounds", amber))
	}
	out = append(out, "")
	out = append(out, t.panel("Directory sizes / "+ageLabel(disk.ObservedAt), rows, w)...)
	rows = []string{t.paint(pad("TYPE", max(12, w-32))+" "+pad("SIZE", 10)+" RECLAIMABLE", muted)}
	for _, d := range disk.Docker {
		rows = append(rows, pad(clean(d["Type"]), max(12, w-32))+" "+pad(clean(d["Size"]), 10)+" "+t.paint(clean(d["Reclaimable"]), cyan))
	}
	if len(disk.Docker) == 0 {
		rows = append(rows, t.paint("No Docker accounting available", muted))
	}
	out = append(out, t.panel("Docker storage", rows, w)...)
	if len(disk.Unavailable) > 0 {
		out = append(out, t.panel("Coverage", m.decorate(disk.Unavailable), w)...)
	}
	return out
}
func (m dashboard) workloadPanel(w int) []string {
	rows := []string{m.theme.paint(pad("PROPERTY", max(12, w-32))+" "+pad("CPU", 8)+" MEMORY", muted)}
	for _, s := range m.sites() {
		sample, ok := m.snapshot.Workloads[s.ID]
		if !ok {
			continue
		}
		name := s.Name
		mem := "—"
		if n := value(sample.Values, "memory_used_bytes"); n != nil {
			mem = console.Bytes(*n)
		}
		rows = append(rows, pad(clean(name), max(12, w-32))+" "+m.theme.paint(pad(percent(value(sample.Values, "cpu_percent")), 8), cyan)+" "+m.theme.paint(mem, violet))
	}
	if len(rows) == 1 {
		rows = append(rows, m.theme.paint("No workload measurements", muted))
	}
	return m.theme.panel("Property resources", rows, w)
}
func (m dashboard) inventoryPanel(w int) []string {
	t := m.theme
	r := m.snapshot.Inventory
	rows := []string{fmt.Sprintf("%d workloads   %d domains   %d routes", len(r.Snapshot.Workloads), len(r.Sites), len(r.Snapshot.Routes)), t.paint(clean(r.Snapshot.OS+" / "+r.Snapshot.Architecture)+"  "+ageLabel(r.Snapshot.ObservedAt), muted)}
	out := t.panel("Discovery", rows, w)
	rows = []string{}
	for _, s := range r.Snapshot.Sources {
		rows = append(rows, t.badge(s.State)+"  "+clean(s.ID)+"  "+t.paint(clean(s.Mode), muted))
		if s.Issue != "" {
			rows = append(rows, wrap([]string{t.paint(clean(s.Issue), amber)}, w-4)...)
		}
	}
	if len(rows) == 0 {
		rows = append(rows, t.paint("No discovery sources recorded", muted))
	}
	out = append(out, t.panel("Sources", rows, w)...)
	rows = []string{}
	for _, s := range r.Snapshot.Workloads {
		rows = append(rows, pad(clean(s.Name), max(12, w-29))+" "+t.badge(s.Status)+"  "+t.paint(clean(s.Type), muted))
	}
	if len(rows) == 0 {
		rows = append(rows, t.paint("No discovered workloads", muted))
	}
	out = append(out, t.panel("Workloads", rows, w)...)
	rows = []string{}
	for _, s := range r.Sites {
		rows = append(rows, t.strong(clean(s.Domain), ink)+"  "+t.badge(s.State))
		for _, route := range s.Routes {
			destination := strings.Join(route.Upstreams, ", ")
			if route.Root != "" {
				destination = route.Root
			}
			if route.RedirectTo != "" {
				destination = route.RedirectTo
			}
			rows = append(rows, wrap([]string{t.paint(clean(route.Kind+" "+strings.Join(route.Paths, ",")+" → "+destination), muted)}, w-4)...)
			if route.Note != "" {
				rows = append(rows, wrap([]string{t.paint(clean(route.Note), amber)}, w-4)...)
			}
		}
	}
	if len(rows) == 0 {
		rows = append(rows, t.paint("No concrete domain routes", muted))
	}
	out = append(out, t.panel("Routes", rows, w)...)
	if len(r.Patterns) > 0 {
		rows = []string{}
		for _, p := range r.Patterns {
			rows = append(rows, clean(strings.Join(p.Hosts, ","))+"  "+t.paint("not enumerable", amber))
		}
		out = append(out, t.panel("Coverage gaps", rows, w)...)
	}
	return out
}
func (m dashboard) toolsPanel(w int) []string {
	descriptions := map[string]string{"security": "Exposure & permissions", "capacity": "History & projections", "impact": "Affected properties", "readiness": "Production evidence", "recovery": "Backups & restores", "preflight": "Maintenance assessment", "changes": "Infrastructure records", "deployments": "Pipeline records", "external": "Watcher observations", "costs": "Billing & allocation"}
	rows := []string{}
	for i, item := range toolMenu {
		line := m.theme.paint(item.Key, cyan) + "  " + pad(item.Label, 18) + " " + m.theme.paint(descriptions[item.Page], muted)
		if i == m.toolSelected && !m.menuFocus {
			line = m.theme.selection(line, w-4)
		}
		rows = append(rows, line)
	}
	return m.theme.panel("Optional tools", rows, w)
}
