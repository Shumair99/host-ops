package tui

import (
	"strings"

	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/model"
)

func reportFindings(r model.Report) []model.Diagnosis {
	if len(r.Diagnoses) > 0 {
		return r.Diagnoses
	}
	return diagnostics.Diagnose(r)
}

func confidenceLabel(confidence string) string {
	switch confidence {
	case "high":
		return "Observed"
	case "medium":
		return "Likely"
	default:
		return "Unconfirmed"
	}
}

func primaryFinding(r model.Report) string {
	if r.Overall == "healthy" {
		return ""
	}
	findings := reportFindings(r)
	for _, confidence := range []string{"high", "medium"} {
		for _, d := range findings {
			if d.Confidence == confidence {
				return confidenceLabel(confidence) + " · " + clean(d.Finding)
			}
		}
	}
	if len(findings) > 0 {
		return "Cause unconfirmed · inspect evidence"
	}
	return ""
}

func (m dashboard) findingRows(findings []model.Diagnosis, w int) []string {
	rows, seen := []string{}, map[string]bool{}
	for _, d := range findings {
		key := d.Finding + "\x00" + d.Investigation
		if seen[key] {
			continue
		}
		seen[key] = true
		label := confidenceLabel(d.Confidence)
		rows = append(rows, wrap([]string{m.theme.strong(label+" · "+clean(d.Finding), amber)}, w-4)...)
		for _, e := range d.Evidence {
			rows = append(rows, wrap([]string{m.theme.paint(clean(e), muted)}, w-4)...)
		}
		rows = append(rows, wrap([]string{m.theme.paint("Next  "+clean(d.Investigation), muted)}, w-4)...)
	}
	return rows
}

func (m dashboard) findingDesk(w int) []string {
	t := m.theme
	rows := []string{}
	reports := reportsByID(m.snapshot.Reports)
	for _, g := range groupSites(m.rawSites()) {
		for _, s := range g.domains() {
			r := domainReport(s, reports)
			if r.Overall == "healthy" {
				continue
			}
			finding := primaryFinding(r)
			if finding == "" {
				continue
			}
			rows = append(rows, clip(t.strong(clean(s.Proxy.Domain), ink)+" · "+t.paint(strings.ToUpper(r.Overall), statusColor(r.Overall)), w-4))
			for i, line := range wrap([]string{t.paint(finding, amber)}, w-8) {
				prefix := "    "
				if i == 0 {
					prefix = "  → "
				}
				rows = append(rows, prefix+line)
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return t.panel("Findings", rows, w)
}
