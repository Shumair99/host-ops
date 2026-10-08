package tui

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/discovery"
	"strings"
)

func inventoryLines(registry discovery.Registry) []string {
	lines := []string{"AUTOMATIC INVENTORY", "Observed: " + console.At(registry.Snapshot.ObservedAt), "OS: " + registry.Snapshot.OS + " / " + registry.Snapshot.Architecture, "", "SOURCES"}
	for _, source := range registry.Snapshot.Sources {
		lines = append(lines, strings.ToUpper(source.State)+" / "+source.Mode+" / "+source.ID)
		if source.Issue != "" {
			lines = append(lines, "  "+source.Issue)
		}
	}
	lines = append(lines, "", "WORKLOADS")
	for _, workload := range registry.Snapshot.Workloads {
		lines = append(lines, workload.Type+" / "+workload.Name+" / "+workload.Status+" / "+workload.Project)
	}
	lines = append(lines, "", "SITE ROUTES")
	for _, site := range registry.Sites {
		lines = append(lines, site.Domain+" / "+site.State)
		for _, route := range site.Routes {
			lines = append(lines, "  "+route.Kind+" "+strings.Join(route.Paths, ",")+" -> "+strings.Join(route.Upstreams, ",")+route.Root)
			if route.Note != "" {
				lines = append(lines, "  "+route.Note)
			}
		}
	}
	lines = append(lines, "", "PATTERNS / COVERAGE")
	for _, route := range registry.Patterns {
		lines = append(lines, strings.Join(route.Hosts, ",")+" / "+route.SourceID+" / individual domains cannot be enumerated")
	}
	return lines
}

func (m dashboard) rawSites() []config.Site {
	if m.snapshot.Sites != nil {
		return m.snapshot.Sites
	}
	return m.source.Config.Sites
}

func (m dashboard) sites() []config.Site {
	sites := []config.Site{}
	for _, group := range groupSites(m.rawSites()) {
		sites = append(sites, group.Primary)
	}
	return sites
}
