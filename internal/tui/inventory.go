package tui

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/discovery"
	"sort"
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
	sites := append([]discovery.Site(nil), registry.Sites...)
	sort.SliceStable(sites, func(i, j int) bool { return config.DomainLess(sites[i].Domain, sites[j].Domain) })
	for _, site := range sites {
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
	var sites []config.Site
	if m.snapshot.Sites != nil {
		sites = config.OrderedSites(m.snapshot.Sites)
	} else {
		sites = config.OrderedSites(m.source.Config.Sites)
	}
	for i := range sites {
		if sites[i].Proxy.Domain != "" {
			sites[i].Name = sites[i].Proxy.Domain
		}
	}
	return sites
}

func (m dashboard) sites() []config.Site {
	sites := []config.Site{}
	for _, group := range groupSites(m.rawSites()) {
		sites = append(sites, group.Primary)
	}
	return sites
}
