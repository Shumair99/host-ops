package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/aiexport"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
)

var aiExportViews = []string{"events", "incidents", "monitoring", "security", "capacity", "impact", "readiness", "recovery", "preflight", "changes", "deployments", "external", "costs"}

type aiMetricSummary struct {
	Metric                string `json:"metric"`
	Samples               int    `json:"samples"`
	Min, Max, P95, Latest float64
}

type aiHistory struct {
	Start, End   time.Time
	SampleLimit  int
	Samples      int
	Statistics   []aiMetricSummary
	Observations []metrics.Sample
}

func summarizeAIHistory(samples []metrics.Sample, end time.Time) aiHistory {
	r := aiHistory{Start: end.Add(-30 * time.Minute), End: end, SampleLimit: 120, Statistics: []aiMetricSummary{}, Observations: []metrics.Sample{}}
	samples = append([]metrics.Sample(nil), samples...)
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].ObservedAt.Before(samples[j].ObservedAt) })
	values := map[string][]float64{}
	for _, sample := range samples {
		if sample.ObservedAt.Before(r.Start) || sample.ObservedAt.After(end) {
			continue
		}
		r.Samples++
		r.Observations = append(r.Observations, sample)
		for key, n := range sample.Values {
			if !math.IsNaN(n) && !math.IsInf(n, 0) {
				values[key] = append(values[key], n)
			}
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := values[key]
		last := v[len(v)-1]
		sort.Float64s(v)
		r.Statistics = append(r.Statistics, aiMetricSummary{Metric: key, Samples: len(v), Min: v[0], Max: v[len(v)-1], P95: v[max(0, int(math.Ceil(float64(len(v))*.95))-1)], Latest: last})
	}
	return r
}

// AIExport gathers every view independently of the UI's filter, selection and
// cache. Existing bounded read-only assessments remain the source of truth.
func (s Source) AIExport(ctx context.Context) (aiexport.Document, error) {
	started := time.Now().UTC()
	d := aiexport.Document{HostID: s.Config.Host.ID, HostName: s.Config.Host.Name, Demo: s.Demo, StartedAt: started}
	snapshot, err := s.Base(ctx)
	if err != nil {
		return d, fmt.Errorf("load dashboard observations: %w", err)
	}
	if snapshot.Sites != nil {
		s.Config.Sites = snapshot.Sites
	}
	counts := map[string]int{}
	labels := map[string]string{}
	for _, site := range snapshot.Sites {
		labels[site.ID] = siteLabel(site)
	}
	checks := []model.Check{}
	findings := []model.Diagnosis{}
	for _, report := range snapshot.Reports {
		counts[report.Overall]++
		for _, check := range report.Checks {
			if check.State == model.Fail || check.State == model.Warn || check.State == model.Unknown {
				checks = append(checks, check)
			}
		}
		findings = append(findings, reportFindings(report)...)
	}
	add := func(title, note string, data any) {
		d.Sections = append(d.Sections, aiexport.Section{Title: title, State: "available", CapturedAt: snapshot.LoadedAt, Note: note, Data: data})
	}
	add("Overview", "Health counts are per observed address; aliases and shared workloads are kept identifiable in Sites and Inventory.", map[string]any{"connection": snapshot.Connection, "health_counts": counts, "domain_labels": labels, "inventory_observed_at": snapshot.Inventory.Snapshot.ObservedAt, "history_notice": snapshot.HistoryNotice, "history_mode": s.Config.Host.HistoryMode})
	histories := map[string]aiHistory{}
	for _, site := range snapshot.Sites {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		records, err := s.Store.Query(ctx, s.Config.Host.ID, site.ID, "metrics", started.Add(-30*time.Minute), 120)
		if err != nil {
			return d, fmt.Errorf("load workload history: %w", err)
		}
		samples := make([]metrics.Sample, 0, len(records))
		for _, record := range records {
			var sample metrics.Sample
			if err := json.Unmarshal(record.Data, &sample); err != nil {
				return d, fmt.Errorf("decode workload history: %w", err)
			}
			samples = append(samples, sample)
		}
		histories[site.ID] = summarizeAIHistory(samples, started)
	}
	add("Sites", "Domain checks remain separate. Resource measurements associated with shared backends must not be added as independent usage. Histories retain at most 120 samples per address in 30 minutes, with statistics.", map[string]any{"sites": snapshot.Sites, "health_reports": snapshot.Reports, "current_workload_metrics": snapshot.Workloads, "workload_history": histories})
	add("Issues", "Only failed, warning and unknown checks appear here; passing evidence is preserved in the health and assessment reports.", map[string]any{"domain_labels": labels, "checks": checks, "diagnoses": findings})
	add("Host metrics", "Metric names carry their units: percent, bytes, seconds or counters. Statistics use recorded samples, not continuous time-weighted observations. Interface traffic includes non-loopback virtual interfaces and may count traffic again.", map[string]any{"current": snapshot.Host, "history": summarizeAIHistory(snapshot.History, started), "network_rate_history": summarizeAIHistory(networkRates(snapshot.History), started)})
	add("Inventory", "Declared dependencies and observed connections retain their evidence types. Missing coverage is not a complete infrastructure map.", snapshot.Inventory)
	if s.Demo && snapshot.Disk == nil {
		disk := DemoDisk()
		snapshot.Disk = &disk
	}
	add("Disk usage", "This uses the retained directory index; exporting does not start another filesystem walk. Partial directory totals are lower bounds; paths and shared storage can overlap.", map[string]any{"report": snapshot.Disk, "scan": snapshot.DiskScan})
	if snapshot.Disk == nil {
		d.Sections[len(d.Sections)-1].State = "unavailable"
	}
	for _, page := range aiExportViews {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		sectionStarted := time.Now().UTC()
		copy := snapshot
		copy.Detail = nil
		sectionCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
		lines, err := s.Evidence(sectionCtx, page, "", "host", &copy)
		cancel()
		section := aiexport.Section{Title: pageLabel(page), State: "available", CapturedAt: sectionStarted, Data: copy.Detail}
		if section.Data == nil && len(lines) > 0 {
			section.Data = map[string]any{"observations": lines}
		}
		if err != nil {
			section.State, section.Note = "unavailable", "Assessment could not finish: "+err.Error()
			if section.Data != nil {
				section.State = "partial"
			}
		}
		switch page {
		case "events", "monitoring", "changes", "deployments", "external":
			section.Note += " Feed scope: latest 100 retained records in the last 24 hours; an empty feed is not proof of continuous health."
		case "readiness":
			section.Note += " Scores measure applicable evidence coverage, not uptime. Confirmed redirect aliases share an assessment; independent workloads do not."
		case "recovery":
			section.Note += " Artifacts and operator-reported successes are not independently verified restores. Targets are not measurements."
		case "preflight":
			section.Note += " Reboot assessment only; no maintenance is performed. Site IDs resolve through the domain labels in Overview and Sites."
		case "costs":
			section.Note += " Estimated workload usage does not reduce the provider bill; unset billing remains unavailable."
		}
		d.Sections = append(d.Sections, section)
	}
	d.FinishedAt = time.Now().UTC()
	return d, nil
}

type aiExportState struct {
	Directory, SavedPath, Error string
	Busy                        bool
	Confirm                     bool
	Bytes, Sections             int
	SavedAt                     time.Time
}

type aiExportMsg struct {
	path            string
	bytes, sections int
	err             error
}

func (m *dashboard) ensureAIExport() {
	if m.aiExport != nil {
		return
	}
	directory, err := aiexport.DefaultDirectory()
	m.aiExport = &aiExportState{Directory: directory}
	if err != nil {
		m.aiExport.Error = "Choose an export folder with P."
	}
}

func (m *dashboard) readyAIExport() bool {
	m.ensureAIExport()
	if m.aiExport.Busy {
		return false
	}
	if m.propagating() {
		m.aiExport.Error = "Wait for the initial observations to finish loading."
		return false
	}
	if m.aiExport.Directory == "" {
		m.aiExport.Error = "Choose an export folder with P."
		return false
	}
	return true
}

func (m *dashboard) requestAIExport() {
	if !m.readyAIExport() {
		return
	}
	m.aiExport.Confirm, m.aiExport.Error = true, ""
	m.menuFocus, m.offset = false, 0
}

func (m *dashboard) cancelAIExportConfirmation() {
	if m.aiExport != nil {
		m.aiExport.Confirm = false
	}
}

func (m *dashboard) startAIExport() tea.Cmd {
	m.ensureAIExport()
	if !m.aiExport.Confirm || m.menuFocus || !m.readyAIExport() {
		return nil
	}
	m.aiExport.Confirm = false
	m.aiExport.Busy, m.aiExport.Error = true, ""
	source, directory := m.source, m.aiExport.Directory
	return m.jobs.command(func() tea.Msg {
		ctx, cancel := context.WithTimeout(m.ctx, 2*time.Minute)
		defer cancel()
		doc, err := source.AIExport(ctx)
		if err != nil {
			return aiExportMsg{err: err}
		}
		data, err := aiexport.Render(doc)
		if err != nil {
			return aiExportMsg{err: err}
		}
		path := filepath.Join(directory, aiexport.Filename(source.Config.Host.ID, time.Now()))
		saved, err := aiexport.Write(ctx, path, data)
		return aiExportMsg{path: saved, bytes: len(data), sections: len(doc.Sections), err: err}
	})
}

func (m dashboard) aiExportLines(w int) []string {
	t := m.theme
	rows := []string{t.strong("A complete handoff for your AI", ink), t.paint("Current observations, all monitoring views and a VPS improvement prompt.", muted), ""}
	state := m.aiExport
	if state != nil && state.Busy {
		rows = append(rows, t.paint("Gathering evidence and preparing Markdown…", cyan), t.paint("Monitoring continues. You can browse other views.", muted))
	} else if state != nil && state.Confirm {
		rows = []string{t.strong("Export the current snapshot?", ink),
			"Save all monitoring data and the AI review prompt as a new Markdown file.",
			t.paint("This only saves a local file in the destination below.", muted), "",
			t.strong("[Enter] Confirm export", cyan), t.paint("[Esc] Cancel", muted)}
	} else {
		rows = append(rows, t.strong("[Enter] Review export", cyan), t.paint("[P] Choose export folder", muted))
	}
	rows = append(rows, "", t.paint("Saved locally. Share the file with the AI you choose.", muted))
	out := t.panel("Export for AI", rows, w)
	directory, err := aiexport.DefaultDirectory()
	if err != nil {
		directory = "Choose a folder with P"
	}
	if state != nil {
		directory = state.Directory
	}
	location := []string{clean(directory)}
	if m.inputKind == "export-directory" {
		location = append(location, "", t.strong("Folder: "+clean(m.input)+"|", cyan), "Enter save · Esc cancel")
	}
	out = append(out, t.panel("Destination", location, w)...)
	if state != nil && state.Error != "" {
		out = append(out, t.panel("Export needs attention", []string{t.paint(clean(state.Error), amber)}, w)...)
	}
	if state != nil && state.SavedPath != "" {
		out = append(out, t.panel("Saved Markdown", []string{t.strong(clean(state.SavedPath), mint), t.paint(fmt.Sprintf("%d views · %.1f KiB · %s", state.Sections, float64(state.Bytes)/1024, state.SavedAt.Local().Format("15:04:05")), muted), "", "Attach this file to your AI conversation and use its included prompt."}, w)...)
	}
	out = append(out, t.panel("Included", []string{"Health, diagnoses, host and workload measurements, routes and storage.", "Security, capacity, dependencies, readiness, recovery and preflight.", "Activity, incidents, deployment records, external observations and costs.", "", "All addresses are included. Unknowns and stale evidence stay labelled.", "Credentials and runtime log bodies are excluded; review before sharing."}, w)...)
	return out
}

func (m *dashboard) editAIExportDirectory() {
	m.ensureAIExport()
	if m.aiExport.Busy {
		return
	}
	m.cancelAIExportConfirmation()
	m.inputKind, m.input, m.menuFocus = "export-directory", m.aiExport.Directory, false
}

func (m *dashboard) setAIExportDirectory(value string) {
	m.ensureAIExport()
	value = strings.TrimSpace(value)
	if value == "" {
		m.aiExport.Error = "Enter a folder path."
		return
	}
	path, err := filepath.Abs(value)
	if err != nil {
		m.aiExport.Error = "Folder path is not valid."
		return
	}
	m.aiExport.Directory, m.aiExport.Error = path, ""
}
