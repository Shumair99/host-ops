package tui

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type recordFeed struct {
	Page    string
	Records []storage.Record
}

func latencyNumber(r model.Report) float64 {
	n := 0.0
	for _, c := range r.Checks {
		if c.Layer == "public_http" {
			n = max(n, c.LatencyMS)
		}
	}
	return n
}

func (m dashboard) issuesWorkspace(w int) []string {
	t := m.theme
	counts := map[model.State]int{}
	layers := map[string]int{}
	affected := 0
	reports := m.groupedReports()
	for _, g := range groupSites(m.rawSites()) {
		r := reports[g.Primary.ID]
		hit := false
		for _, c := range r.Checks {
			if c.State == model.Fail || c.State == model.Warn || c.State == model.Unknown {
				counts[c.State]++
				layers[c.Layer]++
				hit = true
			}
		}
		if hit {
			affected++
		}
	}
	rows := []string{t.strong(fmt.Sprintf("%d affected properties", affected), ink) + "  " + t.paint(fmt.Sprintf("%d failed", counts[model.Fail]), red) + "  " + t.paint(fmt.Sprintf("%d warnings", counts[model.Warn]), amber) + "  " + t.paint(fmt.Sprintf("%d unknown checks", counts[model.Unknown]), muted)}
	keys := []string{}
	total := 0
	for key, count := range layers {
		keys = append(keys, key)
		total += count
	}
	sort.Slice(keys, func(i, j int) bool {
		if layers[keys[i]] != layers[keys[j]] {
			return layers[keys[i]] > layers[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		n := float64(layers[key]) / float64(max(1, total)) * 100
		rows = append(rows, t.paint(pad(layerLabel(key), 16), muted)+t.meter(&n, max(1, w-28), amber)+fmt.Sprintf(" %d", layers[key]))
	}
	if len(keys) == 0 {
		rows = append(rows, t.paint("No failing site checks in current evidence", muted))
	}
	if n := value(m.snapshot.Host.Values, "disk_percent"); n != nil && *n >= 80 {
		rows = append(rows, t.paint("Root disk requires attention · "+percent(n), amber))
	}
	out := t.panel("Attention map", rows, w)
	out = append(m.findingDesk(w), out...)
	out = append(out, t.panel("Exceptions", m.decorate(issueLines(m.snapshot)), w)...)
	next := []string{}
	seen := map[string]bool{}
	for _, r := range m.snapshot.Reports {
		for _, d := range reportFindings(r) {
			key := d.Finding + "\x00" + d.Investigation
			if seen[key] {
				continue
			}
			seen[key] = true
			next = append(next, t.strong(confidenceLabel(d.Confidence)+" · "+clean(d.Finding), amber), t.paint("Next  "+clean(d.Investigation), muted))
		}
	}
	if len(next) > 0 {
		out = append(out, t.panel("Investigation", next, w)...)
	}
	return out
}

func (m dashboard) recordWorkspace(feed recordFeed, w int) []string {
	t := m.theme
	records := append([]storage.Record(nil), feed.Records...)
	sort.SliceStable(records, func(i, j int) bool { return records[i].ObservedAt.After(records[j].ObservedAt) })
	if len(records) > 100 {
		records = records[:100]
	}
	now := time.Now()
	counts := map[string]int{}
	bins := make([]float64, 24)
	for _, r := range records {
		if r.ObservedAt.After(now) || r.ObservedAt.Before(now.Add(-24*time.Hour)) {
			continue
		}
		counts[r.Kind]++
		i := min(23, max(0, 23-int(now.Sub(r.ObservedAt).Hours())))
		bins[i]++
	}
	rows := []string{t.paint(fmt.Sprintf("24h · latest %d retained records · hourly counts", len(records)), muted)}
	peak := 1.0
	points := make([]*float64, 24)
	for i := range bins {
		peak = max(peak, bins[i])
		points[i] = &bins[i]
	}
	// These are counts of the fetched records, not uptime or request volume.
	for _, plot := range barPlot(points, 2, peak) {
		rows = append(rows, t.plot(plot, cyan))
	}
	rows = append(rows, t.paint("−24h                 now", muted))
	kinds := []string{}
	for key := range counts {
		kinds = append(kinds, key)
	}
	sort.Strings(kinds)
	parts := []string{}
	for _, key := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", strings.ReplaceAll(key, "_", " "), counts[key]))
	}
	if len(parts) > 0 {
		rows = append(rows, t.paint(strings.Join(parts, " · "), muted))
	}
	out := t.panel("Recorded activity / "+pageLabel(feed.Page), rows, w)
	if feed.Page == "monitoring" || feed.Page == "external" {
		out = append(out, m.observationMatrix(records, w)...)
	}
	list := []string{}
	day := ""
	for _, r := range records {
		d := r.ObservedAt.Local().Format("Mon 02 Jan")
		if day != d {
			if len(list) > 0 {
				list = append(list, "")
			}
			list = append(list, t.strong(d, ink))
			day = d
		}
		label := recordLabel(r, m.snapshot.Sites)
		summary := activitySummary(r)
		state := ""
		if r.Kind == "deployment" || r.Kind == "incident" || r.Kind == "event" {
			var fields map[string]any
			if json.Unmarshal(r.Data, &fields) == nil {
				state, _ = fields["status"].(string)
				if r.Kind == "event" {
					state, _ = fields["after"].(string)
				}
				if r.Kind == "deployment" {
					health, _ := fields["health_result"].(string)
					if health == "" {
						health = "unknown"
					}
					summary += " · health " + clean(health)
				}
			}
		}
		if r.Kind == "health" || r.Kind == "external_health" {
			var report model.Report
			if json.Unmarshal(r.Data, &report) == nil {
				summary = report.Overall
				state = report.Overall
				if report.Name != "" {
					label = clean(report.Name)
				}
			}
		}
		list = append(list, t.paint(r.ObservedAt.Local().Format("15:04:05"), muted)+"  "+t.paint(label, cyan)+"  "+t.paint(summary, statusColorOrInk(state)))
	}
	if len(list) == 0 {
		message := map[string]string{"events": "No recorded activity", "changes": "No infrastructure changes", "deployments": "No deployment records", "monitoring": "No health observations", "external": "No watcher observations"}[feed.Page]
		list = append(list, message)
	}
	return append(out, t.panel("Evidence feed", list, w)...)
}

func statusColorOrInk(state string) int {
	if state == "" {
		return ink
	}
	return statusColor(state)
}

func (m dashboard) observationMatrix(records []storage.Record, w int) []string {
	t := m.theme
	now := time.Now()
	width := max(1, min(48, w-25))
	type track struct {
		name   string
		states []string
		latest []time.Time
	}
	tracks := map[string]*track{}
	for _, r := range records {
		if r.ObservedAt.After(now) || r.ObservedAt.Before(now.Add(-24*time.Hour)) {
			continue
		}
		var report model.Report
		if json.Unmarshal(r.Data, &report) != nil {
			continue
		}
		id := r.SiteID
		if tracks[id] == nil {
			tracks[id] = &track{name: recordLabel(r, m.snapshot.Sites), states: make([]string, width), latest: make([]time.Time, width)}
		}
		i := min(width-1, max(0, int(math.Floor((24*time.Hour-now.Sub(r.ObservedAt)).Hours()/24*float64(width)))))
		track := tracks[id]
		if track.latest[i].IsZero() || r.ObservedAt.After(track.latest[i]) {
			track.states[i] = report.Overall
			track.latest[i] = r.ObservedAt
		}
	}
	ids := []string{}
	for id := range tracks {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		return config.DomainLess(m.siteOrderKey(ids[i], tracks[ids[i]].name), m.siteOrderKey(ids[j], tracks[ids[j]].name))
	})
	rows := []string{t.paint("Latest observation per time cell · 24h", muted)}
	for _, id := range ids {
		track := tracks[id]
		line := pad(clean(track.name), max(8, w-5-width)) + " "
		for _, state := range track.states {
			mark := "·"
			if state != "" {
				mark = map[string]string{"healthy": "+", "critical": "!", "warning": "~", "unknown": "?"}[state]
				if mark == "" {
					mark = "?"
				}
			}
			line += t.paint(mark, statusColor(state))
		}
		rows = append(rows, line)
	}
	if len(ids) == 0 {
		rows = append(rows, "No recorded health observations")
	}
	rows = append(rows, t.paint("+ healthy  ! critical  ~ warning  ? unknown  · no record", muted))
	return t.panel("Observation map / sampled evidence", rows, w)
}
