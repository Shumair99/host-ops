package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/model"
)

type siteGroup struct {
	Primary config.Site
	Aliases []config.Site
}

func domainKey(domain string) string {
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

// Group only observed www/base pairs. Monitoring and stored domain IDs stay separate.
func groupSites(sites []config.Site) []siteGroup {
	byDomain := map[string]config.Site{}
	for _, site := range sites {
		if site.Proxy.Domain != "" {
			byDomain[domainKey(site.Proxy.Domain)] = site
		}
	}
	groups := []siteGroup{}
	for _, site := range sites {
		domain := domainKey(site.Proxy.Domain)
		if strings.HasPrefix(domain, "www.") {
			if _, exists := byDomain[strings.TrimPrefix(domain, "www.")]; exists {
				continue
			}
		}
		group := siteGroup{Primary: site}
		if domain != "" && !strings.HasPrefix(domain, "www.") {
			if alias, exists := byDomain["www."+domain]; exists {
				group.Aliases = append(group.Aliases, alias)
			}
		}
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Primary.Name) < strings.ToLower(groups[j].Primary.Name)
	})
	return groups
}

func (g siteGroup) domains() []config.Site {
	return append([]config.Site{g.Primary}, g.Aliases...)
}

func (g siteGroup) matches(filter string) bool {
	for _, site := range g.domains() {
		if strings.Contains(strings.ToLower(site.ID+" "+site.Name+" "+site.Proxy.Domain), strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func groupFor(sites []config.Site, id string) siteGroup {
	for _, group := range groupSites(sites) {
		for _, site := range group.domains() {
			if site.ID == id {
				return group
			}
		}
	}
	return siteGroup{}
}

func reportsByID(reports []model.Report) map[string]model.Report {
	byID := map[string]model.Report{}
	for _, report := range reports {
		byID[report.SiteID] = report
	}
	return byID
}

func domainReport(site config.Site, reports map[string]model.Report) model.Report {
	report, ok := reports[site.ID]
	if !ok {
		report = model.Report{SiteID: site.ID, Name: site.Name, Overall: "unknown", Checks: []model.Check{{Layer: "collector", State: model.Unknown, Summary: "No observations yet"}}}
	}
	report.Checks = append([]model.Check(nil), report.Checks...)
	for _, layer := range []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"} {
		found := false
		for _, check := range report.Checks {
			found = found || check.Layer == layer
		}
		if !found {
			report.Checks = append(report.Checks, model.Check{Layer: layer, State: model.Unknown, Summary: "No observations yet"})
		}
	}
	report.Overall = model.Overall(report.Checks)
	return report
}

func groupReport(group siteGroup, reports map[string]model.Report) model.Report {
	combined := domainReport(group.Primary, reports)
	if len(group.Aliases) == 0 {
		return combined
	}
	combined.Checks = append([]model.Check(nil), combined.Checks...)
	for _, site := range group.Aliases {
		report := domainReport(site, reports)
		combined.Checks = append(combined.Checks, report.Checks...)
		if combined.ObservedAt.IsZero() || report.ObservedAt.Before(combined.ObservedAt) {
			combined.ObservedAt = report.ObservedAt
		}
	}
	combined.Overall = model.Overall(combined.Checks)
	return combined
}

func groupProperty(group siteGroup, reports map[string]model.Report) []string {
	primary := domainReport(group.Primary, reports)
	lines := property(group.Primary, primary)
	if len(group.Aliases) == 0 {
		return lines
	}
	combined := groupReport(group, reports)
	lines[0] = console.Safe(group.Primary.Name) + "  [" + strings.ToUpper(combined.Overall) + "]"
	aliasLines := []string{"DOMAIN VARIANTS / INDEPENDENT CHECKS"}
	for _, site := range group.domains() {
		report := domainReport(site, reports)
		aliasLines = append(aliasLines, console.Safe(site.Proxy.Domain)+"  ["+strings.ToUpper(report.Overall)+"]  "+chain(report))
		aliasLines = append(aliasLines, "  Observed: "+console.At(report.ObservedAt)+"  Endpoint: "+console.Safe(unknown(site.Health.PublicURL)))
		if site.ID == group.Primary.ID {
			continue // Primary checks are shown in the existing inspector below.
		}
		for _, check := range report.Checks {
			aliasLines = append(aliasLines, fmt.Sprintf("  [%-7s] %-16s %s", strings.ToUpper(string(check.State)), check.Layer, console.Safe(check.Summary)))
		}
		for _, diagnosis := range report.Diagnoses {
			aliasLines = append(aliasLines, "  Doctor: "+console.Safe(diagnosis.Finding)+" ("+console.Safe(diagnosis.Confidence)+" confidence)")
			for _, evidence := range diagnosis.Evidence {
				aliasLines = append(aliasLines, "  Evidence: "+console.Safe(evidence))
			}
			aliasLines = append(aliasLines, "  Next: "+console.Safe(diagnosis.Investigation))
		}
	}
	aliasLines = append(aliasLines, "", "PRIMARY DOMAIN: "+console.Safe(group.Primary.Proxy.Domain), "")
	return append(append(lines[:4:4], aliasLines...), lines[4:]...)
}

func worseState(a, b model.State) model.State {
	rank := map[model.State]int{model.Skip: 0, model.Pass: 1, model.Warn: 2, model.Unknown: 3, model.Fail: 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
