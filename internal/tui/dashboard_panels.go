package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
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
func (m dashboard) overviewLines(w int) []string  { return m.overviewWorkspace(w) }
func (m dashboard) resourcePanels(w int) []string { return m.resourceCanvas(w) }
func (m dashboard) byteValue(key string) string {
	if n := value(m.snapshot.Host.Values, key); n != nil {
		return console.Bytes(*n)
	}
	return "—"
}
func (m dashboard) bytePair(a, b string) string { return m.byteValue(a) + " / " + m.byteValue(b) }
func (m dashboard) spark(key string, width, color int) []string {
	buckets := recentBuckets(m.snapshot.History, key, width, retainedWindowEnd(m.snapshot.History, time.Now()))
	count := 0
	for _, v := range buckets {
		if v != nil {
			count++
		}
	}
	if count == 0 {
		return []string{m.theme.paint("No samples in last 30m", muted), ""}
	}
	rows := barPlot(buckets, 4, 100)
	for i := range rows {
		rows[i] = m.theme.paint(rows[i], color)
	}
	return rows
}

const overviewTrendWindow = 30 * time.Minute

// Keep history geometry and scale steady while only the live readout changes.
// The axis records the saved sample time; an expired history window stays empty.
func retainedWindowEnd(samples []metrics.Sample, now time.Time) time.Time {
	latest := time.Time{}
	for _, s := range samples {
		if !s.ObservedAt.After(now) && s.ObservedAt.After(latest) {
			latest = s.ObservedAt
		}
	}
	if latest.IsZero() || latest.Before(now.Add(-overviewTrendWindow)) {
		return now
	}
	return latest
}

// recentBuckets aligns the Overview window to fixed time buckets. The grid
// stays still between bucket boundaries, so a redraw cannot make samples jump
// between columns. Missing samples stay blank; values use the real 0-100% scale.
func recentBuckets(samples []metrics.Sample, key string, width int, now time.Time) []*float64 {
	width = max(1, width)
	buckets := make([]*float64, width)
	newest := make([]time.Time, width)
	bucketWidth := overviewTrendWindow / time.Duration(width)
	if bucketWidth < time.Nanosecond {
		bucketWidth = time.Nanosecond
	}
	bucketNanos := int64(bucketWidth)
	currentBucket := now.UnixNano() / bucketNanos
	firstBucket := currentBucket - int64(width)
	start := now.Add(-overviewTrendWindow)
	for _, sample := range samples {
		if sample.ObservedAt.Before(start) || sample.ObservedAt.After(now) {
			continue
		}
		n := value(sample.Values, key)
		if n == nil {
			continue
		}
		sampleBucket := sample.ObservedAt.UnixNano() / bucketNanos
		if sampleBucket < firstBucket || sampleBucket > currentBucket {
			continue
		}
		// The exact rolling window can intersect width+1 fixed buckets at
		// its edges. Fold the two oldest edge buckets into the first cell.
		x := max(0, int(sampleBucket-firstBucket-1))
		if buckets[x] == nil || sample.ObservedAt.After(newest[x]) {
			buckets[x] = n
			newest[x] = sample.ObservedAt
		}
	}
	return buckets
}

func overviewTrendLabel(width int) string {
	if width >= 15 {
		return "Last 30 minutes"
	}
	if width >= 6 {
		return "30 min"
	}
	return "30m"
}

// Full and half blocks are supported by Consolas, Lucida Console and Cascadia
// Mono. A measured zero uses a terminal underline on a space so adjacent zeroes
// connect at the cell baseline; missing observations remain blank.
func barPlot(buckets []*float64, height int, ceiling float64) []string {
	height = max(1, height)
	rows := make([]string, height)
	for row := range rows {
		y := height - row - 1
		var b strings.Builder
		for _, v := range buckets {
			glyph := ' '
			underlineBaseline := false
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
					underlineBaseline = true
				}
			}
			if underlineBaseline {
				b.WriteString("\x1b[4m \x1b[24m")
			} else {
				b.WriteRune(glyph)
			}
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
func (m dashboard) hostLines(w int) []string { return m.hostWorkspace(w) }
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

var checkLabels = map[string]string{"runtime": "Runtime", "local_http": "Local HTTP", "proxy_route": "Proxy route", "dns": "DNS", "tls": "TLS", "tcp": "TCP", "registration": "Registration", "public_http": "Public HTTP", "container_health": "Container health", "collector": "Collector"}

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
		if (c.Layer == "dns" || c.Layer == "tcp" || c.Layer == "tls") && !c.ObservedAt.IsZero() {
			title += m.theme.paint("  "+ageLabel(c.ObservedAt), muted)
		}
		rows = append(rows, title)
		if c.State != model.Pass && c.State != model.Skip {
			rows = append(rows, wrap([]string{m.theme.paint(clean(c.Summary), statusColor(string(c.State)))}, w-4)...)
		}
		if c.State == model.Skip && c.Code != "" {
			rows = append(rows, wrap([]string{m.theme.paint(clean(c.Summary), muted)}, w-4)...)
		}
		if c.Layer == "registration" {
			if c.State == model.Pass {
				rows = append(rows, wrap([]string{m.theme.paint(clean(c.Summary), muted)}, w-4)...)
			}
			for _, detail := range c.Details {
				rows = append(rows, wrap([]string{m.theme.paint(clean(detail), muted)}, w-4)...)
			}
			if !c.ObservedAt.IsZero() {
				rows = append(rows, m.theme.paint("Registry evidence "+ageLabel(c.ObservedAt), muted))
			}
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
	selected := group.Primary
	for _, s := range group.Children {
		if s.ID == m.siteID {
			selected = s
		}
	}
	out := m.inspectorSummary(selected, w)
	if len(m.workloadReadings(group.domains())) > 1 {
		out = append(out, m.theme.panel("Property workloads", m.propertyWorkloadRows(group, w), w)...)
	}
	domains := []config.Site{selected}
	for _, s := range group.domains() {
		if s.ID != selected.ID {
			domains = append(domains, s)
		}
	}
	for _, s := range domains {
		r := domainReport(s, byID)
		rows := []string{m.theme.badge(r.Overall) + m.theme.paint("  "+ageLabel(r.ObservedAt), muted)}
		rows = append(rows, m.checkRows(r.Checks, w)...)
		rows = append(rows, m.theme.paint("Endpoint  "+clean(unknown(s.Health.PublicURL)), muted))
		out = append(out, m.theme.panel(clean(s.Proxy.Domain), rows, w)...)
		out = append(out, "")
		if findings := reportFindings(r); len(findings) > 0 {
			out = append(out, m.theme.panel("Findings / evidence", m.findingRows(findings, w), w)...)
		}
	}
	r := domainReport(selected, byID)
	s := selected
	fields := [][2]string{{"Restart policy", r.Runtime.RestartPolicy}, {"Repository", s.Repository}, {"Branch", s.Branch}, {"Runtime user", r.Runtime.User}}
	rows := []string{}
	for _, f := range fields {
		v := clean(f[1])
		if v == "" {
			v = "—"
		}
		rows = append(rows, wrap([]string{m.theme.paint(pad(f[0], 15), muted) + " " + v}, w-4)...)
	}
	return append(out, m.theme.panel("Deployment identity", rows, w)...)
}
func (m dashboard) workloadPanel(w int) []string {
	rows := []string{m.theme.paint(pad("WORKLOAD", max(12, w-32))+" "+pad("CPU", 8)+" MEMORY", muted)}
	for _, r := range m.workloadReadings(m.rawSites()) {
		sample := r.Sample
		if len(sample.Values) == 0 {
			continue
		}
		name := r.Label
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
	out := m.discoveryTopology(w)
	out = append(out, t.panel("Discovery", rows, w)...)
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
	if w >= 88 {
		a := max(30, (w-1)*2/5)
		compact := []string{}
		for i, item := range toolMenu {
			line := m.theme.paint(item.Key, cyan) + "  " + item.Label
			if i == m.toolSelected && !m.menuFocus {
				line = m.theme.selection(line, a-4)
			}
			compact = append(compact, line)
		}
		selected := toolMenu[min(m.toolSelected, len(toolMenu)-1)]
		detail := []string{m.theme.strong(selected.Label, ink), m.theme.paint(viewHints[selected.Page], muted), ""}
		for _, line := range toolExplanations[selected.Page] {
			detail = append(detail, line)
		}
		detail = append(detail, "", m.theme.paint("Enter open · "+selected.Key+" direct shortcut · Esc return", muted))
		return columns(m.theme.panel("Tools", compact, a), m.theme.panel("Selected tool", detail, w-a-1), a, w-a-1)
	}
	return m.theme.panel("Tools", rows, w)
}
