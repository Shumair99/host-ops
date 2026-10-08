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
	out = append(out, m.systemSummary(w)...)
	out = append(out, "")
	out = append(out, m.overviewSiteTable(w)...)
	events := []string{}
	for _, r := range m.snapshot.Events[:min(4, len(m.snapshot.Events))] {
		label := recordLabel(r, m.snapshot.Sites)
		events = append(events, clip(m.theme.paint(r.ObservedAt.Local().Format("15:04"), muted)+"  "+m.theme.paint(pad(label, min(28, max(12, w/3))), cyan)+"  "+m.theme.paint(activitySummary(r), ink), w-4))
	}
	if len(events) == 0 {
		events = []string{m.theme.paint("No recorded changes in 24h", muted)}
	}
	out = append(out, "")
	title := "Recent activity"
	if len(m.snapshot.Events) > 0 {
		title += " / last change " + ageLabel(m.snapshot.Events[0].ObservedAt)
	}
	return append(out, m.theme.panel(title, events, w)...)
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
	buckets, gaps := relativeBuckets(samples, key, width)
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
		// A half block is a clear, font-safe base level for measured zero use.
		rows[i] = strings.ReplaceAll(rows[i], "_", "▄")
		if i == 0 || i == len(rows)-1 {
			line := []rune(rows[i])
			for x, gap := range gaps {
				if gap {
					line[x] = '|'
				}
			}
			rows[i] = string(line)
		}
		rows[i] = m.theme.paint(rows[i], color)
	}
	return rows
}

// relativeBuckets keeps Overview cards focused on the sequence of recent
// samples. Long collection outages get one visible separator instead of
// consuming most of a short sparkline. Host history retains actual time scale.
func relativeBuckets(samples []metrics.Sample, key string, width int) ([]*float64, []bool) {
	width = max(1, width)
	buckets, gaps := make([]*float64, width), make([]bool, width)
	if len(samples) == 0 {
		return buckets, gaps
	}
	deltas := make([]time.Duration, 0, len(samples)-1)
	for i := 1; i < len(samples); i++ {
		if d := samples[i].ObservedAt.Sub(samples[i-1].ObservedAt); d > 0 {
			deltas = append(deltas, d)
		}
	}
	threshold := 45 * time.Second
	if len(deltas) > 0 {
		sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
		threshold = max(threshold, 3*deltas[len(deltas)/2])
	}
	type segment struct{ start, end int }
	segments := []segment{}
	start := 0
	for i := 1; i < len(samples); i++ {
		if samples[i].ObservedAt.Sub(samples[i-1].ObservedAt) > threshold {
			segments = append(segments, segment{start, i})
			start = i
		}
	}
	segments = append(segments, segment{start, len(samples)})
	if len(segments) > width {
		segments = []segment{{0, len(samples)}}
	}
	gapColumns := min(len(segments)-1, width-len(segments))
	dataColumns := width - gapColumns
	widths := make([]int, len(segments))
	remainingColumns := dataColumns
	remainingSamples := len(samples)
	for i, s := range segments {
		remainingSegments := len(segments) - i - 1
		segmentSamples := s.end - s.start
		segmentWidth := remainingColumns
		if remainingSegments > 0 {
			segmentWidth = int(math.Round(float64(remainingColumns) * float64(segmentSamples) / float64(remainingSamples)))
			segmentWidth = max(1, min(segmentWidth, remainingColumns-remainingSegments))
		}
		widths[i] = segmentWidth
		remainingColumns -= segmentWidth
		remainingSamples -= segmentSamples
	}

	cursor := 0
	for i, s := range segments {
		segmentWidth := widths[i]
		for j := s.start; j < s.end; j++ {
			x := cursor
			if s.end-s.start > 1 && segmentWidth > 1 {
				x += int(math.Round(float64(j-s.start) / float64(s.end-s.start-1) * float64(segmentWidth-1)))
			}
			n := value(samples[j].Values, key)
			if n != nil && (buckets[x] == nil || *n > *buckets[x]) {
				buckets[x] = n
			}
		}
		cursor += segmentWidth
		if i < len(segments)-1 && cursor < width {
			gaps[cursor] = true
			cursor++
		}
	}
	return buckets, gaps
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
