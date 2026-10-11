package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
)

func (m dashboard) readinessSummary(assessments []readiness.Assessment, w int) []string {
	t := m.theme
	rows := []string{t.paint("Evidence coverage, not uptime · skipped checks excluded", muted)}
	start, end := m.assessmentRange(len(assessments))
	for index, a := range assessments[start:end] {
		n := 0.0
		if a.MaxScore > 0 {
			n = float64(a.Score) / float64(a.MaxScore) * 100
		}
		state := "assessed"
		color := mint
		if a.Provisional {
			state = "provisional"
			color = amber
		} else {
			for _, rule := range a.Rules {
				if rule.State == model.Fail || rule.State == model.Warn {
					state, color = "gaps", amber
				}
			}
		}
		line := clip(pad(clean(a.Name), max(8, (w-4)/3))+" "+t.meter(&n, max(1, w-4-max(8, (w-4)/3)-24), color)+fmt.Sprintf(" %.0f%% · %s", n, state), w-4)
		if start+index == m.assessmentSelected && !m.menuFocus {
			line = t.selection(line, w-4)
		}
		rows = append(rows, line)
	}
	if len(assessments) == 0 {
		rows = append(rows, "No property assessments available")
	}
	return t.panel("Readiness / comparison", rows, w)
}

func (m dashboard) recoverySummary(assessments []recovery.Assessment, w int) []string {
	t := m.theme
	rows := []string{t.paint(pad("PROPERTY", max(8, w-41))+" BACKUP   RESTORE  RPO      RTO", muted)}
	start, end := m.assessmentRange(len(assessments))
	for index, a := range assessments[start:end] {
		line := clip(pad(clean(a.Name), max(8, w-41))+" "+t.paint(pad(string(a.BackupState), 8), statusColor(string(a.BackupState)))+" "+t.paint(pad(string(a.RestoreState), 8), statusColor(string(a.RestoreState)))+" "+t.paint(pad(string(a.RPOstate), 8), statusColor(string(a.RPOstate)))+" "+t.paint(string(a.RTOstate), statusColor(string(a.RTOstate))), w-4)
		if start+index == m.assessmentSelected && !m.menuFocus {
			line = t.selection(line, w-4)
		}
		rows = append(rows, line)
	}
	if len(assessments) == 0 {
		rows = append(rows, "No recovery evidence recorded")
	}
	rows = append(rows, t.paint("Unknown = no recorded evidence · targets are not measurements", muted))
	return t.panel("Recovery / evidence matrix", rows, w)
}

func (m dashboard) securitySummary(report security.Report, w int) []string {
	t := m.theme
	counts := map[model.State]int{}
	for _, f := range report.Findings {
		counts[f.State]++
	}
	for _, e := range report.Exposures {
		counts[e.State]++
	}
	applicable := len(report.Findings) + len(report.Exposures) - counts[model.Skip]
	rows := []string{t.paint(fmt.Sprintf("%d posture checks · %d port bindings · shared containers counted once", len(report.Findings)-counts[model.Skip], len(report.Exposures)), muted)}
	for _, state := range []model.State{model.Fail, model.Warn, model.Unknown, model.Pass} {
		n := float64(counts[state]) / float64(max(1, applicable)) * 100
		rows = append(rows, t.paint(pad(string(state), 9), statusColor(string(state)))+t.meter(&n, max(1, w-20), statusColor(string(state)))+fmt.Sprintf(" %d", counts[state]))
	}
	rows = append(rows, t.paint(fmt.Sprintf("%d skipped · %d coverage gaps · %d possible orphan containers", counts[model.Skip], len(report.Unavailable), len(report.PossibleOrphans)), muted))
	rows = append(rows, t.paint("Warnings are hardening opportunities; unknown means unobserved", muted))
	return t.panel("Security / observed posture", rows, w)
}

func (m dashboard) incidentSummary(records []storage.Record, w int) []string {
	t := m.theme
	open, resolved, domains := 0, 0, map[string]bool{}
	layers := map[string]int{}
	for _, r := range records {
		var i incidents.Incident
		if json.Unmarshal(r.Data, &i) != nil {
			continue
		}
		if i.Status == "open" {
			open++
		} else if i.Status == "resolved" {
			resolved++
		}
		for _, id := range i.SiteIDs {
			domains[id] = true
		}
		for _, d := range i.Diagnoses {
			layers[d.Layer]++
		}
	}
	rows := []string{t.paint(fmt.Sprintf("%d open", open), red) + "  " + t.paint(fmt.Sprintf("%d resolved", resolved), mint) + t.paint(fmt.Sprintf("  %d domains in retained incidents", len(domains)), muted)}
	keys := []string{}
	peak := 1
	for key, count := range layers {
		keys = append(keys, key)
		peak = max(peak, count)
	}
	sort.Strings(keys)
	for _, key := range keys {
		n := float64(layers[key]) / float64(peak) * 100
		rows = append(rows, t.paint(pad(layerLabel(key), 16), muted)+t.meter(&n, max(1, w-26), amber)+fmt.Sprintf(" %d", layers[key]))
	}
	rows = append(rows, t.paint("↑↓ selects an incident · w exports its capsule", muted))
	return t.panel("Incident desk", rows, w)
}

func (m dashboard) assessmentCount() int {
	if m.page == "readiness" {
		if v, ok := m.snapshot.Detail.([]readiness.Assessment); ok {
			return len(v)
		}
	}
	if m.page == "recovery" {
		if v, ok := m.snapshot.Detail.([]recovery.Assessment); ok {
			return len(v)
		}
	}
	if m.page == "incidents" {
		if v, ok := m.snapshot.Detail.([]storage.Record); ok {
			return len(v)
		}
	}
	return 0
}

func (m dashboard) assessmentRange(length int) (int, int) {
	limit := max(1, min(8, m.bodyHeight()/3))
	selected := min(m.assessmentSelected, max(0, length-1))
	start := max(0, selected-limit+1)
	return start, min(length, start+limit)
}

func (m dashboard) incidentList(records []storage.Record, w int) []string {
	t := m.theme
	rows := []string{}
	start, end := m.assessmentRange(len(records))
	for index, r := range records[start:end] {
		var incident incidents.Incident
		if json.Unmarshal(r.Data, &incident) != nil {
			continue
		}
		line := t.paint(pad(incident.Status, 9), statusColor(incident.Status)) + " " + t.paint(incident.StartedAt.Local().Format("02 Jan 15:04"), muted) + "  " + m.incidentProperties(incident.SiteIDs) + t.paint(" · "+clean(incident.Dependency), muted)
		if start+index == m.assessmentSelected && !m.menuFocus {
			line = t.selection(line, w-4)
		}
		rows = append(rows, line)
	}
	if len(records) == 0 {
		rows = append(rows, "No recorded incidents")
	}
	return t.panel("Incident records", rows, w)
}

func (m dashboard) incidentProperties(ids []string) string {
	labels := timelineLabels(m.rawSites())
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := labels[id]
		if name == "" {
			name = clean(id)
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func (m dashboard) siteOrderKey(id, fallback string) string {
	for _, s := range m.rawSites() {
		if s.ID == id && s.Proxy.Domain != "" {
			return s.Proxy.Domain
		}
	}
	return fallback
}

func (m dashboard) orderedReadiness(v []readiness.Assessment) []readiness.Assessment {
	v = append([]readiness.Assessment(nil), v...)
	for i := range v {
		if len(v[i].Aliases) == 0 {
			v[i].Name = m.siteOrderKey(v[i].SiteID, v[i].Name)
		}
	}
	sort.SliceStable(v, func(i, j int) bool {
		return config.DomainLess(m.siteOrderKey(v[i].SiteID, v[i].Name), m.siteOrderKey(v[j].SiteID, v[j].Name))
	})
	return v
}

func (m dashboard) orderedRecovery(v []recovery.Assessment) []recovery.Assessment {
	v = append([]recovery.Assessment(nil), v...)
	for i := range v {
		v[i].Name = m.siteOrderKey(v[i].SiteID, v[i].Name)
	}
	sort.SliceStable(v, func(i, j int) bool {
		return config.DomainLess(m.siteOrderKey(v[i].SiteID, v[i].Name), m.siteOrderKey(v[j].SiteID, v[j].Name))
	})
	return v
}

func (m dashboard) securityScope(f security.Finding) string {
	for _, s := range m.rawSites() {
		if s.ID == f.SiteID {
			label := siteLabel(s)
			if len(f.SiteIDs) > 1 {
				label += fmt.Sprintf(" · %d endpoints", len(f.SiteIDs))
			}
			if f.Container != "" {
				label += " / " + clean(f.Container)
			}
			return label
		}
	}
	if f.Container != "" {
		return clean(f.Container)
	}
	return clean(f.SiteID)
}
