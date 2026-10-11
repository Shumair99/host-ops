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

// Instruments use fixed time buckets and explicit units. Blank cells mean no
// observation; neither a connection gap nor an old reading becomes a zero.
func (m dashboard) hostHistory() []metrics.Sample {
	out := append([]metrics.Sample(nil), m.snapshot.History...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ObservedAt.Before(out[j].ObservedAt) })
	return out
}

func measuredStats(samples []metrics.Sample, key string, now time.Time) (count int, low, peak, p95 float64) {
	values := []float64{}
	for _, s := range samples {
		if s.ObservedAt.After(now) || s.ObservedAt.Before(now.Add(-overviewTrendWindow)) {
			continue
		}
		if n := value(s.Values, key); n != nil {
			values = append(values, *n)
		}
	}
	if len(values) == 0 {
		return
	}
	sort.Float64s(values)
	return len(values), values[0], values[len(values)-1], values[max(0, int(math.Ceil(float64(len(values))*.95))-1)]
}

func networkRates(samples []metrics.Sample) []metrics.Sample {
	out := make([]metrics.Sample, len(samples))
	for i, s := range samples {
		out[i] = metrics.Sample{ObservedAt: s.ObservedAt, Values: map[string]float64{}}
		if i == 0 {
			continue
		}
		prev := samples[i-1]
		seconds := s.ObservedAt.Sub(prev.ObservedAt).Seconds()
		if seconds <= 0 || seconds > 180 {
			continue
		}
		// A reboot or changed interface set invalidates cumulative differences.
		a, b := value(prev.Values, "network_interfaces"), value(s.Values, "network_interfaces")
		if a == nil || b == nil || *a != *b {
			continue
		}
		a, b = value(prev.Values, "network_interface_identity"), value(s.Values, "network_interface_identity")
		if a == nil || b == nil || *a != *b {
			continue
		}
		u, v := value(prev.Values, "uptime_seconds"), value(s.Values, "uptime_seconds")
		if u == nil || v == nil || *v < *u {
			continue
		}
		for _, dir := range []string{"rx", "tx"} {
			a, b := value(prev.Values, "network_"+dir+"_bytes"), value(s.Values, "network_"+dir+"_bytes")
			if a != nil && b != nil && *b >= *a {
				out[i].Values["network_"+dir+"_rate"] = (*b - *a) / seconds
			}
		}
	}
	return out
}

func signalValue(n *float64, unit string) string {
	if n == nil {
		return "—"
	}
	switch unit {
	case "%":
		return percent(n)
	case "B/s":
		return console.Bytes(*n) + "/s"
	case "B":
		return console.Bytes(*n)
	default:
		return fmt.Sprintf("%.2f%s", *n, unit)
	}
}

func (m dashboard) signalPanel(label, key, unit string, color int, ceiling float64, samples []metrics.Sample, current *float64, w, height int) []string {
	t := m.theme
	now := retainedWindowEnd(samples, time.Now())
	count, low, peak, p95 := measuredStats(samples, key, now)
	rows := []string{t.strong(signalValue(current, unit), metricColorForUnit(current, unit, color)) + t.paint("  current", muted)}
	if count == 0 {
		rows = append(rows, t.paint("Waiting for measured history", muted))
		for len(rows) < height+3 {
			rows = append(rows, "")
		}
		return t.panel(label, rows, w)
	}
	if ceiling <= 0 {
		ceiling = max(1, peak*1.12)
	}
	axis := max(8, len(signalValue(&ceiling, unit))+1)
	plotWidth := max(1, w-4-axis)
	for i, row := range barPlot(recentBuckets(samples, key, plotWidth, now), height, ceiling) {
		label := ""
		if i == 0 {
			label = signalValue(&ceiling, unit)
		}
		if i == height-1 {
			zero := 0.0
			label = signalValue(&zero, unit)
		}
		rows = append(rows, t.paint(pad(label, axis-1)+"│", muted)+t.plot(row, color))
	}
	endLabel := now.Local().Format("15:04")
	rows = append(rows, t.paint(pad("−30m", axis)+pad("", max(0, plotWidth-len(endLabel)))+endLabel, muted))
	stats := "p95 " + signalValue(&p95, unit) + "  peak " + signalValue(&peak, unit)
	if w >= 66 {
		stats += "  min " + signalValue(&low, unit) + fmt.Sprintf("  n=%d", count)
	}
	rows = append(rows, t.paint(clip(stats, w-4), muted))
	return t.panel(label, rows, w)
}

func metricColorForUnit(n *float64, unit string, color int) int {
	if unit == "%" {
		return metricColor(n, color)
	}
	if n == nil {
		return muted
	}
	return color
}

func (m dashboard) resourceCanvas(w int) []string {
	t, samples := m.theme, m.hostHistory()
	end := retainedWindowEnd(samples, time.Now())
	rows := []string{}
	if w < 48 {
		for _, spec := range []struct {
			label, key string
			color      int
		}{{"CPU", "cpu_percent", cyan}, {"Memory", "memory_percent", violet}, {"Root disk", "disk_percent", amber}} {
			n := value(m.snapshot.Host.Values, spec.key)
			c := metricColor(n, spec.color)
			rows = append(rows, t.paint(pad(spec.label, 9), muted)+t.meter(n, max(1, w-24), c)+" "+percent(n))
		}
		rows = append(rows, t.paint("Disk free "+m.byteValue("disk_free_bytes"), muted))
		return t.panel("Host pulse / current", rows, w)
	}
	plotWidth := max(1, w-24)
	for _, spec := range []struct {
		label, key string
		color      int
	}{{"CPU", "cpu_percent", cyan}, {"Memory", "memory_percent", violet}, {"Root disk", "disk_percent", amber}} {
		n := value(m.snapshot.Host.Values, spec.key)
		c := metricColor(n, spec.color)
		count, _, _, p95 := measuredStats(samples, spec.key, end)
		stats := ""
		if count > 0 && w >= 54 {
			stats = "  p95 " + percent(&p95)
		}
		rows = append(rows, t.paint(pad(spec.label, 10), muted)+t.strong(pad(percent(n), 8), c)+t.paint(stats, muted))
		if count == 0 {
			rows = append(rows, t.paint("           No samples in last 30m", muted), "")
		} else {
			for index, plot := range barPlot(recentBuckets(samples, spec.key, plotWidth, end), 3, 100) {
				axis := []string{"     100% │", "          │", "       0% │"}[index]
				rows = append(rows, t.paint(axis, edge)+t.plot(plot, spec.color))
			}
		}
	}
	rows = append(rows, t.paint("           −30m"+strings.Repeat(" ", max(0, plotWidth-10))+end.Local().Format("15:04"), muted))
	rows = append(rows, t.paint(clip("RAM "+m.bytePair("memory_used_bytes", "memory_total_bytes")+" · disk free "+m.byteValue("disk_free_bytes"), w-4), muted))
	return t.panel("Host pulse / 30 minutes", rows, w)
}

func stateMark(state model.State) string {
	return map[model.State]string{model.Pass: "+", model.Fail: "!", model.Warn: "~", model.Unknown: "?", model.Skip: "−"}[state]
}

func (m dashboard) healthMatrix(w int, limit int) []string {
	t := m.theme
	sites := append([]siteGroup(nil), groupSites(m.rawSites())...)
	reports := m.groupedReports()
	sort.SliceStable(sites, func(i, j int) bool {
		return stateRank(reports[sites[i].Primary.ID].Overall) < stateRank(reports[sites[j].Primary.ID].Overall)
	})
	layers := []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"}
	cellWidth := 4
	if w-4 < 49 {
		cellWidth = 3
	}
	if w-4 < 43 {
		cellWidth = 2
	}
	nameWidth := max(8, w-4-9-cellWidth*len(layers))
	header := pad("PROPERTY", nameWidth) + " " + pad("HEALTH", 8)
	for _, label := range []string{"RUN", "LOC", "PXY", "DNS", "TLS", "WEB"} {
		if cellWidth < 4 {
			label = label[:1]
		}
		header += pad(" "+label, cellWidth)
	}
	rows := []string{clip(m.fleetSummary(), w-4), t.paint(header, muted)}
	for i, g := range sites {
		if i >= limit {
			break
		}
		r := reports[g.Primary.ID]
		line := t.paint(pad(propertyLabel(g), nameWidth), statusColor(r.Overall))
		line += " " + t.paint(pad(strings.ToUpper(r.Overall), 8), statusColor(r.Overall))
		for _, layer := range layers {
			state := layerState(r, layer)
			line += pad(" "+t.strong(stateMark(state), statusColor(string(state))), cellWidth)
		}
		rows = append(rows, clip(line, w-4))
	}
	if len(sites) == 0 {
		rows = append(rows, t.paint("No discovered properties yet", muted))
	}
	if len(sites) > limit {
		rows = append(rows, t.paint(fmt.Sprintf("+ %d more properties", len(sites)-limit), muted))
	}
	rows = append(rows, t.paint("+ pass  ! fail  ~ warn  ? unknown  − n/a", muted))
	return t.panel("Health matrix", rows, w)
}

func stateRank(state string) int {
	switch state {
	case "critical", "fail":
		return 0
	case "warning", "warn":
		return 1
	case "unknown", "":
		return 2
	case "healthy", "pass":
		return 3
	default:
		return 4
	}
}

func (m dashboard) latencyPanel(w, limit int) []string {
	t := m.theme
	type reading struct {
		name  string
		ms    float64
		state model.State
	}
	readings := []reading{}
	reports := m.groupedReports()
	for _, s := range m.sites() {
		r := reports[s.ID]
		ms := latencyNumber(r)
		for _, c := range r.Checks {
			if c.Layer == "public_http" && c.LatencyMS == ms && ms > 0 {
				readings = append(readings, reading{siteLabel(s), ms, c.State})
				break
			}
		}
	}
	sort.SliceStable(readings, func(i, j int) bool { return readings[i].ms > readings[j].ms })
	rows := []string{t.paint("Slowest endpoint per property", muted)}
	ceiling := 1.0
	for _, r := range readings {
		ceiling = max(ceiling, r.ms)
	}
	nameWidth, barWidth := max(8, (w-16)/2), max(1, (w-16)/2)
	for i, r := range readings {
		if i >= limit {
			break
		}
		n := r.ms / ceiling * 100
		rows = append(rows, clip(pad(clean(r.name), nameWidth)+" "+t.meter(&n, barWidth, statusColor(string(r.state)))+fmt.Sprintf(" %4.0fms", r.ms), w-4))
	}
	if len(readings) == 0 {
		rows = append(rows, t.paint("No measured HTTP response times", muted))
	}
	rows = append(rows, t.paint("Bars relative to slowest measured check", muted))
	return t.panel("Public response", rows, w)
}

func (m dashboard) pressureRanking(w, limit int, metric string) []string {
	t := m.theme
	type reading struct {
		name  string
		value float64
	}
	readings := []reading{}
	for _, r := range m.workloadReadings(m.rawSites()) {
		if n := value(r.Sample.Values, metric); n != nil {
			readings = append(readings, reading{r.Label, *n})
		}
	}
	sort.SliceStable(readings, func(i, j int) bool { return readings[i].value > readings[j].value })
	unit, label, c := "%", "CPU · busiest first", cyan
	if metric == "memory_used_bytes" {
		unit, label, c = "B", "Memory · largest first", violet
	}
	rows := []string{t.paint(label, muted)}
	ceiling := 1.0
	for _, r := range readings {
		ceiling = max(ceiling, r.value)
	}
	nameWidth := max(8, (w-17)/2)
	barWidth := max(1, w-4-nameWidth-12)
	for i, r := range readings {
		if i >= limit {
			break
		}
		n := r.value / ceiling * 100
		rows = append(rows, clip(pad(clean(r.name), nameWidth)+" "+t.meter(&n, barWidth, c)+" "+signalValue(&r.value, unit), w-4))
	}
	if len(readings) == 0 {
		rows = append(rows, t.paint("No workload resource measurements", muted))
	}
	rows = append(rows, t.paint("Bars relative to largest observed workload", muted))
	return t.panel("Workload pressure", rows, w)
}

func (m dashboard) overviewWorkspace(w int) []string {
	out := m.systemSummary(w)
	if w >= 88 {
		a := (w - 1) / 2
		out = append(out, columns(m.resourceCanvas(a), m.healthMatrix(w-a-1, 8), a, w-a-1)...)
		out = append(out, columns(m.latencyPanel(a, 4), m.pressureRanking(w-a-1, 4, "memory_used_bytes"), a, w-a-1)...)
	} else {
		limit := 5
		if m.bodyHeight() < 20 {
			limit = 3
		}
		out = append(out, m.healthMatrix(w, limit)...)
		out = append(out, m.resourceCanvas(w)...)
		out = append(out, m.latencyPanel(w, 4)...)
		out = append(out, m.pressureRanking(w, 4, "memory_used_bytes")...)
	}
	return append(out, m.operationsFeed(w, 4, 3)...)
}

func (m dashboard) hostWorkspace(w int) []string {
	t, samples := m.theme, m.hostHistory()
	out := m.systemSummary(w)
	panels := func(left, right func(int) []string) {
		if w >= 76 {
			a := (w - 1) / 2
			out = append(out, columns(left(a), right(w-a-1), a, w-a-1)...)
		} else {
			out = append(out, left(w)...)
			out = append(out, right(w)...)
		}
	}
	panels(func(a int) []string {
		return m.signalPanel("CPU utilization", "cpu_percent", "%", cyan, 100, samples, value(m.snapshot.Host.Values, "cpu_percent"), a, 4)
	}, func(a int) []string {
		return m.signalPanel("Memory utilization", "memory_percent", "%", violet, 100, samples, value(m.snapshot.Host.Values, "memory_percent"), a, 4)
	})
	rates := networkRates(samples)
	current := map[string]float64{}
	if !m.snapshot.Host.ObservedAt.IsZero() && len(m.snapshot.Host.Values) > 0 {
		// The numeric rate can use the live counter. History bars cannot.
		liveSamples := append([]metrics.Sample(nil), samples...)
		if len(liveSamples) == 0 || m.snapshot.Host.ObservedAt.After(liveSamples[len(liveSamples)-1].ObservedAt) {
			liveSamples = append(liveSamples, m.snapshot.Host)
		}
		liveRates := networkRates(liveSamples)
		if len(liveRates) > 0 {
			current = liveRates[len(liveRates)-1].Values
		}
	}
	panels(func(a int) []string {
		return m.signalPanel("Interface receive", "network_rx_rate", "B/s", cyan, 0, rates, value(current, "network_rx_rate"), a, 3)
	}, func(a int) []string {
		return m.signalPanel("Interface transmit", "network_tx_rate", "B/s", mint, 0, rates, value(current, "network_tx_rate"), a, 3)
	})
	out = append(out, t.paint(clip("Traffic sums non-loopback interfaces; bridges / virtual interfaces can count the same packet again.", w), muted))
	out = append(out, m.networkCountersPanel(w)...)
	panels(func(a int) []string { return m.schedulerPanel(a) }, func(a int) []string { return m.memoryPanel(a) })
	panels(func(a int) []string { return m.kernelPressurePanel(a) }, func(a int) []string {
		return m.signalPanel("Root filesystem", "disk_percent", "%", amber, 100, samples, value(m.snapshot.Host.Values, "disk_percent"), a, 3)
	})
	panels(func(a int) []string { return m.pressureRanking(a, 8, "cpu_percent") }, func(a int) []string { return m.pressureRanking(a, 8, "memory_used_bytes") })
	if len(m.snapshot.Host.Unavailable) > 0 {
		rows := []string{}
		for _, reason := range m.snapshot.Host.Unavailable {
			rows = append(rows, t.paint("Unavailable · "+clean(reason), amber))
		}
		out = append(out, t.panel("Collection coverage", rows, w)...)
	}
	return out
}

func (m dashboard) networkCountersPanel(w int) []string {
	t, v := m.theme, m.snapshot.Host.Values
	rows := []string{}
	for _, dir := range []string{"rx", "tx"} {
		fields := []string{strings.ToUpper(dir) + " total " + m.byteValue("network_"+dir+"_bytes")}
		for _, key := range []string{"packets", "errors", "dropped"} {
			n := value(v, "network_"+dir+"_"+key)
			label := "—"
			if n != nil {
				label = fmt.Sprintf("%.0f", *n)
			}
			fields = append(fields, key+" "+label)
		}
		rows = append(rows, strings.Join(fields, " · "))
	}
	return t.panel("Interface counters / cumulative since reset", rows, w)
}

func (m dashboard) schedulerPanel(w int) []string {
	t, v := m.theme, m.snapshot.Host.Values
	rows := []string{}
	cores := value(v, "cpu_count")
	for _, pair := range [][2]string{{"1 minute", "load_1"}, {"5 minutes", "load_5"}, {"15 minutes", "load_15"}} {
		n := value(v, pair[1])
		var ratio *float64
		if n != nil && cores != nil && *cores > 0 {
			r := *n / *cores * 100
			ratio = &r
		}
		rows = append(rows, t.paint(pad(pair[0], 10), muted)+" "+t.meter(ratio, max(1, w-25), metricColor(ratio, cyan))+" "+signalValue(n, ""))
	}
	coreLabel := "—"
	if cores != nil {
		coreLabel = fmt.Sprintf("%.0f", *cores)
	}
	rows = append(rows, "Logical CPUs "+coreLabel+" · bars at CPU count")
	for _, pair := range [][2]string{{"Running tasks", "tasks_running"}, {"Blocked tasks", "tasks_blocked"}, {"Total tasks", "tasks_total"}} {
		n := value(v, pair[1])
		s := "—"
		if n != nil {
			s = fmt.Sprintf("%.0f", *n)
		}
		rows = append(rows, t.paint(pad(pair[0], 16), muted)+s)
	}
	return t.panel("Scheduler / load", rows, w)
}

func (m dashboard) memoryPanel(w int) []string {
	t, v := m.theme, m.snapshot.Host.Values
	rows := []string{t.meter(value(v, "memory_percent"), max(1, w-13), violet) + " " + percent(value(v, "memory_percent"))}
	for _, pair := range [][2]string{{"Used / total", m.bytePair("memory_used_bytes", "memory_total_bytes")}, {"Available", m.byteValue("memory_available_bytes")}, {"Page cache", m.byteValue("memory_cached_bytes")}, {"Swap / total", m.bytePair("swap_used_bytes", "swap_total_bytes")}} {
		rows = append(rows, t.paint(pad(pair[0], 15), muted)+pair[1])
	}
	rows = append(rows, t.paint("Cache overlaps reclaimable memory", muted))
	return t.panel("Memory / allocation", rows, w)
}

func (m dashboard) kernelPressurePanel(w int) []string {
	t := m.theme
	rows := []string{t.paint("Time stalled · avg over 10 seconds", muted)}
	for _, resource := range []string{"cpu", "memory", "io"} {
		n := value(m.snapshot.Host.Values, "pressure_"+resource+"_some_percent")
		label := "—"
		if n != nil {
			label = fmt.Sprintf("%.2f%%", *n)
		}
		rows = append(rows, t.paint(pad(strings.ToUpper(resource), 8), muted)+t.meter(n, max(1, w-22), metricColor(n, cyan))+" "+label)
	}
	rows = append(rows, t.paint("Some tasks stalled · 0–100% scale", muted))
	return t.panel("Kernel pressure", rows, w)
}
