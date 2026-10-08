// Package console renders operational evidence as readable ASCII text.
package console

import (
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/preflight"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/storage"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

func Safe(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r >= 32 && r <= 126:
			b.WriteRune(r)
		case unicode.IsControl(r):
		case r == '—' || r == '–':
			b.WriteByte('-')
		case r == '→':
			b.WriteString("->")
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
func Bytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}
func Percent(value *float64) string {
	if value == nil {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%.1f%%", *value)
}
func Money(value *float64, currency string) string {
	if value == nil {
		return "UNKNOWN"
	}
	return fmt.Sprintf("%s %.2f", currency, *value)
}
func At(t time.Time) string {
	if t.IsZero() {
		return "unobserved"
	}
	return t.Local().Format("02 Jan 15:04:05")
}
func Bar(values map[string]float64, key, label string) string {
	value, ok := values[key]
	if !ok {
		return fmt.Sprintf("%-6s [????????????????????] UNKNOWN", label)
	}
	n := int(math.Round(value / 5))
	n = max(0, min(20, n))
	return fmt.Sprintf("%-6s [%s%s] %5.1f%%", label, strings.Repeat("#", n), strings.Repeat("-", 20-n), value)
}
func FormatJSON(kind string, data []byte) []string {
	var value any
	switch kind {
	case "disk":
		value = &metrics.DiskReport{}
	case "metrics":
		value = &metrics.Sample{}
	case "capacity":
		value = &capacity.Report{}
	case "costs":
		value = &costs.Report{}
	case "readiness":
		value = &[]readiness.Assessment{}
	case "recovery":
		value = &[]recovery.Assessment{}
	case "preflight":
		value = &preflight.Report{}
	case "impact":
		value = &preflight.Impact{}
	case "history", "events", "deployments", "diff", "incidents":
		value = &[]storage.Record{}
	default:
		var generic any
		if json.Unmarshal(data, &generic) != nil {
			return []string{Safe(string(data))}
		}
		return Generic(generic)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return []string{"Evidence could not be rendered."}
	}
	switch v := value.(type) {
	case *metrics.DiskReport:
		return Format(kind, *v)
	case *metrics.Sample:
		return Format(kind, *v)
	case *capacity.Report:
		return Format(kind, *v)
	case *costs.Report:
		return Format(kind, *v)
	case *[]readiness.Assessment:
		return Format(kind, *v)
	case *[]recovery.Assessment:
		return Format(kind, *v)
	case *preflight.Report:
		return Format(kind, *v)
	case *preflight.Impact:
		return Format(kind, *v)
	case *[]storage.Record:
		return Format(kind, *v)
	}
	return nil
}
func Format(kind string, value any) []string {
	lines := []string{}
	add := func(format string, args ...any) { lines = append(lines, Safe(fmt.Sprintf(format, args...))) }
	switch v := value.(type) {
	case metrics.Sample:
		add("HOST %s  |  Observed %s", v.HostID, At(v.ObservedAt))
		add("%s", Bar(v.Values, "cpu_percent", "CPU"))
		add("%s", Bar(v.Values, "memory_percent", "RAM"))
		add("%s", Bar(v.Values, "disk_percent", "DISK"))
		keys := []string{}
		for k := range v.Values {
			if k == "disk_observed_unix_seconds" || k == "cpu_percent" || k == "memory_percent" || k == "disk_percent" {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if strings.HasSuffix(k, "bytes") {
				add("%-25s %s", strings.ReplaceAll(k, "_", " "), Bytes(v.Values[k]))
			} else {
				add("%-25s %.2f", strings.ReplaceAll(k, "_", " "), v.Values[k])
			}
		}
		for _, s := range v.Unavailable {
			add("UNKNOWN: %s", s)
		}
	case metrics.DiskReport:
		add("DISK ACCOUNTING  |  %s", At(v.ObservedAt))
		add("Read-only. Parent and child directory totals overlap.")
		add("Incomplete measurements are lower bounds.")
		add("")
		add("%-14s %s", "MEASURED SIZE", "DIRECTORY")
		for _, e := range v.Directories {
			prefix := ""
			if e.Partial {
				prefix = ">= "
			}
			add("%-14s %s", prefix+Bytes(float64(e.Bytes)), e.Path)
		}
		add("")
		add("%-17s %-14s %s", "DOCKER STORAGE", "SIZE", "POTENTIALLY RECLAIMABLE")
		for _, d := range v.Docker {
			add("%-17s %-14s %s", d["Type"], d["Size"], d["Reclaimable"])
		}
		for _, s := range v.Unavailable {
			add("UNKNOWN: %s", s)
		}
		add("No files, images, logs or volumes were removed.")
	case []readiness.Assessment:
		for _, a := range v {
			add("%s  READINESS %d/%d", a.Name, a.Score, a.MaxScore)
			if a.Provisional {
				add("PROVISIONAL: missing evidence earns no points.")
			}
			for _, rule := range a.Rules {
				add("[%s] %s (%d/%d)", strings.ToUpper(string(rule.State)), rule.Title, rule.Points, rule.MaxPoints)
				add("  %s", rule.Evidence)
				if rule.State != "pass" {
					add("  Next: %s", rule.Investigation)
				}
			}
			add("")
		}
	case []recovery.Assessment:
		for _, a := range v {
			add("%s  |  confidence: %s", a.Name, a.Confidence)
			add("Backup %-8s Restore %-8s", a.BackupState, a.RestoreState)
			add("RPO %dh: %-8s RTO %dm: %-8s", a.RPOhours, a.RPOstate, a.RTOminutes, a.RTOstate)
			if a.LastBackup != nil {
				add("Last backup: %s %s %s", a.LastBackup.ID, a.LastBackup.Status, At(a.LastBackup.CompletedAt))
			}
			if a.LastRestore != nil {
				add("Last restore: %s %s %s", a.LastRestore.ID, a.LastRestore.Status, At(a.LastRestore.CompletedAt))
			}
			for _, n := range a.Notes {
				add("  %s", n)
			}
			add("")
		}
	case costs.Report:
		if v.TotalCost == nil {
			add("OPTIONAL BILLING / COST SETUP")
			add("No usable monthly budget is connected. No allocation is assumed.")
			add("API providers: hostops billing list --provider hetzner (or hostinger)")
			add("Connect one resource: hostops billing set --provider NAME --resource ID")
			add("Manual budget: hostops billing set --provider manual --monthly AMOUNT --currency GBP")
			add("Own hardware: hostops billing set --provider self-hosted (budget optional)")
			for _, note := range v.Notes {
				add("%s", note)
			}
			break
		}
		add("MONTHLY HOST COST: %s", Money(v.TotalCost, v.Currency))
		if v.Billing != nil {
			add("Source: %s / %s | %s", v.Billing.Provider, v.Billing.Source, At(v.Billing.ObservedAt))
			if v.Billing.Plan != "" {
				add("Plan: %s", v.Billing.Plan)
			}
			if v.Billing.ServerMonthToDate != nil {
				add("Server-only month-to-date estimate: %s", Money(v.Billing.ServerMonthToDate, v.Currency))
			}
		}
		fees := false
		for _, site := range v.Sites {
			fees = fees || site.Revenue != nil
		}
		if fees {
			add("%-20s %-15s %-15s %s", "SITE", "ALLOCATED COST", "HOSTING FEE", "MARGIN")
		} else {
			add("%-20s %s", "SITE", "ESTIMATED ALLOCATION")
		}
		for _, s := range v.Sites {
			if fees {
				add("%-20s %-15s %-15s %s", s.Name, Money(s.Cost, v.Currency), Money(s.Revenue, v.Currency), Money(s.Margin, v.Currency))
			} else {
				add("%-20s %s", s.Name, Money(s.Cost, v.Currency))
			}
			if s.NoisyNeighbour {
				add("  Review disproportionate memory consumption.")
			}
		}
		if !fees {
			add("Hosting fees/margins are hidden until you configure client fees.")
		}
		add("")
		add("%s", v.Formula)
		add("Weights: equal %.2f memory %.2f CPU %.2f", v.Weights.Equal, v.Weights.Memory, v.Weights.CPU)
		for _, n := range v.Notes {
			add("%s", n)
		}
	case capacity.Report:
		add("CAPACITY [%s]  |  %d retained samples", strings.ToUpper(string(v.State)), v.Samples)
		add("CPU p95: %s  RAM p95: %s", Percent(v.CPU95), Percent(v.Memory95))
		add("Root disk: %s", Percent(v.DiskPercent))
		if v.FreeBytes != nil {
			add("Available: %s", Bytes(*v.FreeBytes))
		}
		if v.AlreadyAbove85 {
			add("ATTENTION: disk is already above 85%%.")
		} else if v.DaysTo85 != nil {
			add("Projection to 85%%: %.1f days", *v.DaysTo85)
		} else {
			add("Projection: UNKNOWN (insufficient history or no growth)")
		}
		for _, n := range v.Notes {
			add("%s", n)
		}
	case preflight.Report:
		add("PREFLIGHT %s [%s]", v.Target, strings.ToUpper(v.Overall))
		add("%s", v.Recommendation)
		add("Assessment only; no maintenance action is executed.")
		for _, c := range v.Checks {
			add("[%s] %s %s", strings.ToUpper(string(c.State)), c.SiteID, c.Layer)
			add("  %s", c.Summary)
		}
		lines = append(lines, Format("impact", v.Impact)...)
	case preflight.Impact:
		add("RESOURCE: %s", v.Resource)
		add("REGISTERED BLAST RADIUS: %d properties", len(v.Affected))
		for _, s := range v.Affected {
			add("  %s  %s -> %s", s.ID, s.Proxy.Domain, s.Proxy.Upstream)
		}
		for _, n := range v.Notes {
			add("%s", n)
		}
	case []storage.Record:
		if len(v) == 0 {
			add("No recorded evidence yet.")
		}
		for _, r := range v {
			if r.Kind == "incident" {
				var incident incidents.Incident
				if json.Unmarshal(r.Data, &incident) == nil {
					add("[%s] %s | %s", strings.ToUpper(incident.Status), incident.ID, strings.Join(incident.SiteIDs, ", "))
					add("Started: %s | Source: %s | Dependency: %s", At(incident.StartedAt), incident.Source, incident.Dependency)
					if incident.ResolvedAt != nil {
						add("Resolved: %s", At(*incident.ResolvedAt))
					}
					for _, diagnosis := range incident.Diagnoses {
						add("%s (%s confidence)", diagnosis.Finding, diagnosis.Confidence)
						for _, evidence := range diagnosis.Evidence {
							add("  Evidence: %s", evidence)
						}
						add("  Next: %s", diagnosis.Investigation)
					}
					add("Symptoms recorded when the outage opened:")
					for _, symptom := range incident.Symptoms {
						add("[%s] %s: %s", strings.ToUpper(string(symptom.State)), symptom.Layer, symptom.Summary)
					}
					add("")
					continue
				}
			}
			add("%s  %s  %s  #%d", At(r.ObservedAt), r.SiteID, r.Kind, r.ID)
			var data any
			if json.Unmarshal(r.Data, &data) == nil {
				for _, s := range Generic(data) {
					add("  %s", s)
				}
			}
			add("")
		}
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return []string{"Evidence unavailable."}
		}
		var decoded any
		json.Unmarshal(data, &decoded)
		lines = Generic(decoded)
	}
	return lines
}
func Generic(value any) []string {
	lines := []string{}
	var walk func(any, string)
	walk = func(v any, indent string) {
		switch n := v.(type) {
		case map[string]any:
			keys := []string{}
			for k := range n {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				label := strings.ReplaceAll(k, "_", " ")
				switch n[k].(type) {
				case map[string]any, []any:
					lines = append(lines, indent+strings.ToUpper(label)+":")
					walk(n[k], indent+"  ")
				default:
					if n[k] == nil {
						lines = append(lines, indent+label+": UNKNOWN")
					} else {
						lines = append(lines, indent+label+": "+fmt.Sprint(n[k]))
					}
				}
			}
		case []any:
			if len(n) == 0 {
				lines = append(lines, indent+"(none)")
			}
			for _, item := range n {
				walk(item, indent)
				lines = append(lines, "")
			}
		case nil:
			lines = append(lines, indent+"UNKNOWN")
		default:
			lines = append(lines, indent+fmt.Sprint(n))
		}
	}
	walk(value, "")
	for i := range lines {
		lines[i] = Safe(lines[i])
	}
	return lines
}
