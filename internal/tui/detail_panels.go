package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/discovery"
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
	case recordFeed:
		return m.recordWorkspace(v, w)
	case capacity.Report:
		rows := []string{t.badge(string(v.State)) + t.paint(fmt.Sprintf("  %d samples", v.Samples), muted)}
		for _, spec := range []struct {
			label string
			n     *float64
			color int
		}{{"CPU p95", v.CPU95, cyan}, {"Memory p95", v.Memory95, violet}, {"Root disk", v.DiskPercent, amber}} {
			rows = append(rows, t.paint(pad(spec.label, 12), muted)+t.meter(spec.n, max(1, w-27), metricColor(spec.n, spec.color))+" "+percent(spec.n))
		}
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
		out := append(panel("Capacity / retained evidence", rows), panel("Storage projection", []string{t.paint(growth, muted), t.paint(forecast, amber)})...)
		return append(out, m.signalPanel("Recent disk utilization / 30m", "disk_percent", "%", amber, 100, m.hostHistory(), value(m.snapshot.Host.Values, "disk_percent"), w, 4)...)
	case costs.Report:
		if v.TotalCost == nil {
			source := "Not configured"
			if m.source.Config.Host.Billing != nil {
				source = clean(m.source.Config.Host.Billing.Provider)
			}
			rows := []string{"Billing source  " + t.paint(source, muted)}
			if m.source.Config.Host.Billing == nil {
				rows = append(rows, "Add your monthly budget or connect your provider above.")
			} else if !m.source.Config.Host.BillingEnabled() {
				rows = append(rows, "Cost tracking is off. Add a budget to enable allocation.")
			} else {
				rows = append(rows, "Pricing is unavailable. Open billing settings to reconnect.")
			}
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
		rows = append(rows, t.paint("Usage estimates do not reduce the hosting bill.", muted))
		out := panel("Host budget / estimate", rows)
		usagePercent := func(n *float64) string {
			if n == nil {
				return "Unknown"
			}
			return fmt.Sprintf("%.1f%%", *n*100)
		}
		rows = []string{"CPU  " + usagePercent(v.HostCPUUsage) + "   RAM  " + usagePercent(v.HostMemoryUsage)}
		if v.CapacityUsage != nil {
			n := *v.CapacityUsage * 100
			rows = append(rows, t.meter(&n, max(1, w-24), cyan)+" "+usagePercent(v.CapacityUsage)+" used")
		}
		rows = append(rows, fmt.Sprintf("Measured site use     %s  (%d/%d workloads)", console.Money(v.SiteCost, v.Currency), v.MeasuredSites, len(v.Sites)))
		if v.OtherCost != nil {
			rows = append(rows, "Shared / unmeasured   "+console.Money(v.OtherCost, v.Currency))
		} else if v.UnallocatedCost != nil {
			rows = append(rows, "Unassigned budget     "+console.Money(v.UnallocatedCost, v.Currency), t.paint("Includes shared usage and spare capacity.", muted))
		}
		rows = append(rows, "Unused capacity       "+console.Money(v.UnusedCost, v.Currency))
		rows = append(rows, t.paint(fmt.Sprintf("Usage weighting: CPU %.0f%% / RAM %.0f%%", v.Weights.CPU*100, v.Weights.Memory*100), muted))
		if v.HostSamples > 0 {
			rows = append(rows, t.paint(fmt.Sprintf("Recent %s · %d host samples", v.WindowEnd.Sub(v.WindowStart).Round(time.Second), v.HostSamples), muted))
		}
		out = append(out, panel("Capacity / monthly usage estimate", rows)...)
		fees := false
		for _, s := range v.Sites {
			fees = fees || s.Revenue != nil
		}
		nameWidth := max(10, w-20)
		if fees {
			nameWidth = max(10, w-42)
		}
		heading := pad("PROPERTY", nameWidth) + " USAGE COST"
		if fees {
			heading += "     FEE         MARGIN"
		}
		rows = []string{t.paint(heading, muted)}
		for _, s := range v.Sites {
			line := pad(clean(m.siteOrderKey(s.SiteID, s.Name)), nameWidth) + " " + t.paint(pad(console.Money(s.Cost, v.Currency), 12), cyan)
			if fees {
				line += " " + pad(console.Money(s.Revenue, v.Currency), 11) + " " + console.Money(s.Margin, v.Currency)
			}
			rows = append(rows, line)
			if s.CapacityUsage != nil {
				n := *s.CapacityUsage * 100
				rows = append(rows, t.meter(&n, max(1, w-33), cyan)+" "+usagePercent(s.CapacityUsage)+" of host capacity")
			} else {
				rows = append(rows, t.paint("Usage measurements unavailable", muted))
			}
			if len(s.SiteIDs) > 1 {
				rows = append(rows, t.paint(fmt.Sprintf("Shared workload · %d addresses", len(s.SiteIDs)), muted))
			}
			if s.NoisyNeighbour {
				rows = append(rows, t.paint("High memory share", amber))
			}
		}
		out = append(out, panel("Sites / measured usage", rows)...)
		notes := []string{}
		if v.Billing != nil {
			notes = append(notes, v.Billing.Notes...)
		}
		for _, note := range v.Notes {
			if strings.HasPrefix(note, "Hosting fees need") {
				notes = append(notes, "Fees hidden / currency mismatch")
			} else if strings.HasPrefix(note, "Fresh host") || strings.HasPrefix(note, "Workload totals exceed") || strings.HasPrefix(note, "Site and host observations") || strings.HasPrefix(note, "Legacy equal weight") {
				notes = append(notes, note)
			}
		}
		notes = append(notes, "Recent CPU/RAM usage projected to a month; disk, traffic and backup usage excluded.")
		if len(notes) > 0 {
			out = append(out, panel("Billing evidence", m.decorate(notes))...)
		}
		return out
	case []readiness.Assessment:
		v = m.orderedReadiness(v)
		out := m.readinessSummary(v, w)
		if len(v) == 0 {
			return out
		}
		for _, a := range v[min(m.assessmentSelected, len(v)-1) : min(m.assessmentSelected, len(v)-1)+1] {
			ratio := 0.0
			if a.MaxScore > 0 {
				ratio = float64(a.Score) / float64(a.MaxScore) * 100
			}
			state := "Evidence score"
			if a.Provisional {
				state = "Provisional / missing evidence"
			}
			rows := []string{t.meter(&ratio, max(1, w-16), cyan) + fmt.Sprintf(" %.0f%%", ratio), t.paint(fmt.Sprintf("%s · %d/%d applicable points", state, a.Score, a.MaxScore), muted)}
			if len(a.Aliases) > 0 {
				rows = append(rows, wrap([]string{t.paint("Additional addresses: "+strings.Join(a.Aliases, ", "), muted)}, w-4)...)
			}
			passed, missing, failed, skipped := 0, 0, 0, 0
			for _, r := range a.Rules {
				if r.State == "pass" {
					passed++
				} else if r.State == "unknown" {
					missing += r.MaxPoints
				} else if r.State == "skip" {
					skipped++
				} else {
					failed += r.MaxPoints
				}
			}
			rows = append(rows, t.paint(fmt.Sprintf("%d passed rules · missing %d pts · unmet %d pts · %d skipped", passed, missing, failed, skipped), muted))
			for _, r := range a.Rules {
				if (r.State == "pass" || r.State == "skip") && !m.showPassing {
					continue
				}
				rows = append(rows, t.badge(string(r.State))+"  "+clean(r.Title))
				if r.State != "pass" && r.State != "skip" {
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
		v = m.orderedRecovery(v)
		out := m.recoverySummary(v, w)
		if len(v) == 0 {
			return out
		}
		for _, a := range v[min(m.assessmentSelected, len(v)-1) : min(m.assessmentSelected, len(v)-1)+1] {
			rows := []string{"Backup  " + t.badge(string(a.BackupState)) + "   Restore  " + t.badge(string(a.RestoreState)), fmt.Sprintf("Data-loss target (RPO) %dh  ", a.RPOhours) + t.badge(string(a.RPOstate)), fmt.Sprintf("Recovery-time target (RTO) %dm  ", a.RTOminutes) + t.badge(string(a.RTOstate)), t.paint("Confidence  "+clean(a.Confidence), muted)}
			if a.LastBackup != nil {
				rows = append(rows, "Backup  "+clean(a.LastBackup.ID)+"  "+t.badge(a.LastBackup.Status)+"  "+ageLabel(a.LastBackup.CompletedAt))
			}
			if a.LastRestore != nil {
				rows = append(rows, "Restore  "+clean(a.LastRestore.ID)+"  "+t.badge(a.LastRestore.Status)+"  "+ageLabel(a.LastRestore.CompletedAt))
			}
			if a.LastBackup == nil && a.LastRestore == nil {
				rows = append(rows, wrap([]string{t.paint("Backup job reports are discovered automatically from ~/.local/state/hostops/recovery", muted)}, w-4)...)
			}
			if len(a.Artifacts) > 0 {
				rows = append(rows, "", t.strong(fmt.Sprintf("%d local backup artifacts detected", len(a.Artifacts)), cyan))
				artifacts := append([]discovery.BackupArtifact(nil), a.Artifacts...)
				sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].ModifiedAt.After(artifacts[j].ModifiedAt) })
				for _, artifact := range artifacts[:min(5, len(artifacts))] {
					rows = append(rows, wrap([]string{clean(artifact.Path)}, w-4)...)
					rows = append(rows, t.paint(artifact.Format+" · "+console.Bytes(float64(artifact.SizeBytes))+" · modified "+ageLabel(artifact.ModifiedAt), muted))
				}
			}
			for _, issue := range a.DiscoveryIssues {
				rows = append(rows, wrap([]string{t.paint(clean(issue), amber)}, w-4)...)
			}
			rows = append(rows, t.paint("A backup file alone does not prove a successful restore", muted))
			out = append(out, panel(clean(a.Name), rows)...)
		}
		return out
	case security.Report:
		out := m.securitySummary(v, w)
		rows := []string{}
		lastScope := ""
		for _, f := range v.Findings {
			if (f.State == "pass" || f.State == "skip") && !m.showPassing {
				continue
			}
			scope := m.securityScope(f)
			if scope != lastScope {
				if len(rows) > 0 {
					rows = append(rows, "")
				}
				rows = append(rows, wrap([]string{t.strong(scope, cyan)}, w-4)...)
				lastScope = scope
			}
			rows = append(rows, t.badge(string(f.State))+"  "+clean(f.Title))
			if f.Evidence != "" {
				rows = append(rows, wrap([]string{t.paint(clean(f.Evidence), muted)}, w-4)...)
			}
			if f.State != "pass" && f.Investigation != "" {
				rows = append(rows, wrap([]string{t.paint("Next  "+clean(f.Investigation), muted)}, w-4)...)
			}
		}
		if len(rows) == 0 {
			message := "No posture evidence"
			if len(v.Findings) > 0 {
				message = "No failing posture findings · 1 shows passing evidence"
			}
			rows = []string{t.paint(message, muted)}
		}
		out = append(out, panel("Runtime posture", rows)...)
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
		rows := []string{t.badge(v.Overall), t.paint(clean(v.Recommendation), amber), t.paint("Checks startup, health, capacity and recovery evidence; executes no maintenance", muted)}
		rows = append(rows, m.preflightRows(v.Checks, v.Impact.Affected, w)...)
		return append(panel("Maintenance / "+clean(v.Target), rows), m.impactPanel(v.Impact, w)...)
	case preflight.Impact:
		return m.impactPanel(v, w)
	case []storage.Record:
		out := m.incidentSummary(v, w)
		out = append(out, m.incidentList(v, w)...)
		if len(v) == 0 {
			return out
		}
		for _, r := range v[min(m.assessmentSelected, len(v)-1) : min(m.assessmentSelected, len(v)-1)+1] {
			var i incidents.Incident
			if json.Unmarshal(r.Data, &i) != nil {
				continue
			}
			rows := []string{t.badge(i.Status) + "  " + m.incidentProperties(i.SiteIDs), t.paint("Started  "+console.At(i.StartedAt), muted), "Dependency  " + clean(i.Dependency)}
			if i.ResolvedAt != nil {
				rows = append(rows, t.paint("Resolved  "+console.At(*i.ResolvedAt), muted))
			}
			rows = append(rows, m.findingRows(i.Diagnoses, w)...)
			rows = append(rows, m.checkRows(i.Symptoms, w)...)
			out = append(out, panel(clean(i.ID), rows)...)
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
	if v.Graph != nil && len(v.Graph.Nodes) > 0 {
		return m.topologyPanel(v, w)
	}
	rows := []string{m.theme.strong(fmt.Sprintf("%d affected properties", len(groupSites(v.Affected))), amber)}
	rows = append(rows, m.theme.paint("Properties → application runtimes · grouped by recorded network", muted))
	if proxy := m.source.Config.Host.CaddyContainer; proxy != "" {
		rows = append(rows, m.theme.paint("Shared proxy: "+clean(proxy), muted))
	}
	groups := map[string][]string{}
	for _, g := range groupSites(v.Affected) {
		network := g.Primary.Runtime.Network
		if network == "" {
			network = "Network not recorded"
		}
		runtime := g.Primary.Runtime.Container
		if runtime == "" {
			runtime = g.Primary.Runtime.Unit
		}
		if runtime == "" {
			runtime = g.Primary.Runtime.Type
		}
		groups[network] = append(groups[network], siteLabel(g.Primary)+" → "+clean(unknown(runtime)))
		seen := map[string]bool{runtime: true}
		for _, child := range g.Children {
			backend := child.Runtime.Container
			if backend != "" && !seen[backend] {
				childNetwork := child.Runtime.Network
				if childNetwork == "" {
					childNetwork = "Network not recorded"
				}
				groups[childNetwork] = append(groups[childNetwork], "  "+clean(child.Proxy.Domain)+" → "+clean(backend))
				seen[backend] = true
			}
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows = append(rows, m.theme.strong(clean(v.Resource), ink))
	for index, key := range keys {
		branch := "├─ "
		stem := "│  "
		if index == len(keys)-1 {
			branch = "└─ "
			stem = "   "
		}
		rows = append(rows, branch+m.theme.paint(clean(key), cyan))
		for i, line := range groups[key] {
			leaf := "├─ "
			if i == len(groups[key])-1 {
				leaf = "└─ "
			}
			rows = append(rows, stem+leaf+line)
		}
	}
	if len(v.Notes) > 0 {
		rows = append(rows, m.theme.paint("Routing inventory; hidden database/service dependencies may add impact", muted))
	}
	return m.theme.panel("Dependency / "+clean(v.Resource), rows, w)
}
