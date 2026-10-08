package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/preflight"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
)

func (m dashboard) detailLines(w int) []string {
	t := m.theme
	panel := func(title string, rows []string) []string { return t.panel(title, rows, w) }
	switch v := m.snapshot.Detail.(type) {
	case capacity.Report:
		rows := []string{t.badge(string(v.State)) + t.paint(fmt.Sprintf("  %d samples", v.Samples), muted), "CPU p95     " + t.paint(percent(v.CPU95), cyan), "Memory p95  " + t.paint(percent(v.Memory95), violet), "Root disk   " + t.paint(percent(v.DiskPercent), amber)}
		if v.FreeBytes != nil {
			rows = append(rows, "Free space  "+console.Bytes(*v.FreeBytes))
		}
		growth := "Insufficient history"
		if v.DiskGrowthBytesPerDay != nil {
			growth = console.Bytes(*v.DiskGrowthBytesPerDay) + " / day"
		}
		forecast := "Insufficient history / no growth"
		if v.AlreadyAbove85 {
			forecast = "Already above 85%"
		} else if v.DaysTo85 != nil {
			forecast = fmt.Sprintf("%.1f days to 85%%", *v.DaysTo85)
		}
		return append(panel("Capacity", rows), panel("Storage projection", []string{t.paint(growth, muted), t.paint(forecast, amber)})...)
	case costs.Report:
		if v.TotalCost == nil {
			source := "Not configured"
			if m.source.Config.Host.Billing != nil {
				source = clean(m.source.Config.Host.Billing.Provider)
			}
			rows := []string{"Billing source  " + t.paint(source, muted), "Monthly budget  " + t.paint("Not available", muted), "Allocation      " + t.paint("Awaiting billing settings", muted)}
			// Provider/access failures are evidence and must remain visible.
			for _, note := range v.Notes {
				if strings.HasPrefix(note, "Billing unavailable:") {
					rows = append(rows, wrap([]string{t.paint(clean(note), amber)}, w-4)...)
				}
			}
			return panel("Optional billing", rows)
		}
		rows := []string{t.strong(console.Money(v.TotalCost, v.Currency)+" / month", cyan)}
		if v.Billing != nil {
			rows = append(rows, t.paint(clean(v.Billing.Provider+" / "+v.Billing.Plan), muted), t.paint(clean(v.Billing.Source)+"  "+ageLabel(v.Billing.ObservedAt), muted))
			if v.Billing.ServerMonthToDate != nil {
				rows = append(rows, "Month to date  "+console.Money(v.Billing.ServerMonthToDate, v.Currency))
			}
		}
		out := panel("Host budget / estimate", rows)
		fees := false
		for _, s := range v.Sites {
			fees = fees || s.Revenue != nil
		}
		nameWidth := max(10, w-20)
		if fees {
			nameWidth = max(10, w-42)
		}
		heading := pad("PROPERTY", nameWidth) + " ALLOCATION"
		if fees {
			heading += "     FEE         MARGIN"
		}
		rows = []string{t.paint(heading, muted)}
		for _, s := range v.Sites {
			line := pad(clean(s.Name), nameWidth) + " " + t.paint(pad(console.Money(s.Cost, v.Currency), 12), cyan)
			if fees {
				line += " " + pad(console.Money(s.Revenue, v.Currency), 11) + " " + console.Money(s.Margin, v.Currency)
			}
			rows = append(rows, line)
			if s.NoisyNeighbour {
				rows = append(rows, t.paint("High memory share", amber))
			}
		}
		out = append(out, panel("Estimated allocation", rows)...)
		notes := []string{}
		if v.Billing != nil {
			notes = append(notes, v.Billing.Notes...)
		}
		for _, note := range v.Notes {
			if strings.HasPrefix(note, "Hosting fees need") {
				notes = append(notes, "Fees hidden / currency mismatch")
			}
		}
		if len(notes) > 0 {
			out = append(out, panel("Billing evidence", m.decorate(notes))...)
		}
		return out
	case []readiness.Assessment:
		out := []string{}
		for _, a := range v {
			ratio := 0.0
			if a.MaxScore > 0 {
				ratio = float64(a.Score) / float64(a.MaxScore) * 100
			}
			state := "Evidence score"
			if a.Provisional {
				state = "Provisional / missing evidence"
			}
			rows := []string{t.meter(&ratio, max(1, w-16), cyan) + fmt.Sprintf(" %d/%d", a.Score, a.MaxScore), t.paint(state, muted)}
			for _, r := range a.Rules {
				rows = append(rows, t.badge(string(r.State))+"  "+clean(r.Title))
				if r.State != "pass" {
					rows = append(rows, wrap([]string{t.paint(clean(r.Evidence), muted)}, w-4)...)
					if r.Investigation != "" {
						rows = append(rows, wrap([]string{t.paint("Next  "+clean(r.Investigation), muted)}, w-4)...)
					}
				}
			}
			out = append(out, panel(clean(a.Name), rows)...)
			out = append(out, "")
		}
		return out
	case []recovery.Assessment:
		out := []string{}
		for _, a := range v {
			rows := []string{"Backup  " + t.badge(string(a.BackupState)) + "   Restore  " + t.badge(string(a.RestoreState)), fmt.Sprintf("RPO %dh  ", a.RPOhours) + t.badge(string(a.RPOstate)) + fmt.Sprintf("   RTO %dm  ", a.RTOminutes) + t.badge(string(a.RTOstate)), t.paint("Confidence  "+clean(a.Confidence), muted)}
			if a.LastBackup != nil {
				rows = append(rows, "Backup  "+clean(a.LastBackup.ID)+"  "+t.badge(a.LastBackup.Status)+"  "+ageLabel(a.LastBackup.CompletedAt))
			}
			if a.LastRestore != nil {
				rows = append(rows, "Restore  "+clean(a.LastRestore.ID)+"  "+t.badge(a.LastRestore.Status)+"  "+ageLabel(a.LastRestore.CompletedAt))
			}
			out = append(out, panel(clean(a.Name), rows)...)
		}
		return out
	case security.Report:
		rows := []string{}
		for _, f := range v.Findings {
			rows = append(rows, t.badge(string(f.State))+"  "+t.paint(clean(f.SiteID), cyan)+"  "+clean(f.Title))
			if f.Evidence != "" {
				rows = append(rows, wrap([]string{t.paint(clean(f.Evidence), muted)}, w-4)...)
			}
			if f.State != "pass" && f.Investigation != "" {
				rows = append(rows, wrap([]string{t.paint("Next  "+clean(f.Investigation), muted)}, w-4)...)
			}
		}
		if len(rows) == 0 {
			rows = []string{t.paint("No posture evidence", muted)}
		}
		out := panel("Runtime posture", rows)
		rows = []string{}
		for _, e := range v.Exposures {
			rows = append(rows, t.badge(string(e.State))+"  "+clean(e.Container)+"  "+t.paint(clean(e.HostIP+":"+e.HostPort), cyan))
			if e.Evidence != "" {
				rows = append(rows, wrap([]string{t.paint(clean(e.Evidence), muted)}, w-4)...)
			}
		}
		if len(rows) == 0 {
			rows = []string{t.paint("No port exposure evidence", muted)}
		}
		out = append(out, panel("Published ports", rows)...)
		if len(v.Listening) > 0 {
			out = append(out, panel("Listening sockets", m.decorate(v.Listening))...)
		}
		if len(v.PossibleOrphans) > 0 {
			rows = []string{}
			for _, c := range v.PossibleOrphans {
				rows = append(rows, clean(c.Name)+"  "+t.badge(c.State)+"  "+clean(c.Image))
			}
			out = append(out, panel("Unmapped containers", rows)...)
		}
		if len(v.Unavailable) > 0 {
			out = append(out, panel("Coverage gaps", m.decorate(v.Unavailable))...)
		}
		return out
	case preflight.Report:
		rows := []string{t.badge(v.Overall), t.paint(clean(v.Recommendation), amber)}
		rows = append(rows, m.checkRows(v.Checks, w)...)
		return append(panel("Maintenance / "+clean(v.Target), rows), m.impactPanel(v.Impact, w)...)
	case preflight.Impact:
		return m.impactPanel(v, w)
	case []storage.Record:
		out := []string{}
		for _, r := range v {
			var i incidents.Incident
			if json.Unmarshal(r.Data, &i) != nil {
				continue
			}
			rows := []string{t.badge(i.Status) + "  " + clean(strings.Join(i.SiteIDs, ", ")), t.paint("Started  "+console.At(i.StartedAt), muted), "Dependency  " + clean(i.Dependency)}
			if i.ResolvedAt != nil {
				rows = append(rows, t.paint("Resolved  "+console.At(*i.ResolvedAt), muted))
			}
			for _, d := range i.Diagnoses {
				rows = append(rows, wrap([]string{t.strong(clean(d.Finding), amber)}, w-4)...)
				rows = append(rows, m.decorate(d.Evidence)...)
				rows = append(rows, wrap([]string{t.paint("Next  "+clean(d.Investigation), muted)}, w-4)...)
			}
			rows = append(rows, m.checkRows(i.Symptoms, w)...)
			out = append(out, panel(clean(i.ID), rows)...)
		}
		if len(out) == 0 {
			return panel("Incidents", []string{t.paint("No recorded incidents", muted)})
		}
		return out
	}
	rows := m.decorate(m.lines)
	if len(rows) == 0 {
		rows = []string{t.paint("No recorded evidence", muted)}
	}
	return panel(pageLabel(m.page), rows)
}
func (m dashboard) impactPanel(v preflight.Impact, w int) []string {
	rows := []string{m.theme.strong(fmt.Sprintf("%d affected properties", len(groupSites(v.Affected))), amber)}
	for _, g := range groupSites(v.Affected) {
		rows = append(rows, clean(g.Primary.Name)+"  "+m.theme.paint(clean(g.Primary.Proxy.Domain), cyan))
	}
	if len(v.Notes) > 0 {
		rows = append(rows, m.theme.paint("Coverage: observed dependencies", muted))
	}
	return m.theme.panel("Dependency / "+clean(v.Resource), rows, w)
}
