package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/x/ansi"
)

func TestToolsPairDomainsUsingDomainIdentityRatherThanGeneratedIDs(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.Sites = []config.Site{
		{ID: "a-hash", Name: "www.z.example", Proxy: config.Proxy{Domain: "www.z.example"}},
		{ID: "b-hash", Name: "www.a.example", Proxy: config.Proxy{Domain: "www.a.example"}},
		{ID: "c-hash", Name: "Brand Z", Proxy: config.Proxy{Domain: "z.example"}},
		{ID: "d-hash", Name: "a.example", Proxy: config.Proxy{Domain: "a.example"}},
	}
	ready, recoveries, records := []readiness.Assessment{}, []recovery.Assessment{}, []storage.Record{}
	registry := discovery.Registry{}
	for _, s := range m.snapshot.Sites {
		ready = append(ready, readiness.Assessment{SiteID: s.ID, Name: s.Name})
		recoveries = append(recoveries, recovery.Assessment{SiteID: s.ID, Name: s.Name})
		data, _ := json.Marshal(model.Report{Name: s.Name, Overall: "healthy"})
		records = append(records, storage.Record{SiteID: s.ID, ObservedAt: time.Now().Add(-time.Hour), Data: data})
		registry.Sites = append(registry.Sites, discovery.Site{ID: s.ID, Domain: s.Proxy.Domain})
	}
	ready = m.orderedReadiness(ready)
	recoveries = m.orderedRecovery(recoveries)
	for i, id := range []string{"d-hash", "b-hash", "c-hash", "a-hash"} {
		if ready[i].SiteID != id || recoveries[i].SiteID != id {
			t.Fatal("assessment lists split www from its normal endpoint")
		}
	}
	for _, text := range []string{ansi.Strip(strings.Join(m.observationMatrix(records, 100), "\n")), strings.Join(inventoryLines(registry), "\n")} {
		last := -1
		for _, name := range []string{"a.example", "www.a.example", "z.example", "www.z.example"} {
			// The observation map honors the custom display name.
			if strings.Contains(text, "Brand Z") && name == "z.example" {
				name = "Brand Z"
			}
			index := strings.Index(text, name)
			if index <= last {
				t.Fatal("domain rows were not paired", text)
			}
			last = index
		}
	}
}

func TestSecurityUsesDomainNameAndShowsSharedScopeOnce(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.Sites = []config.Site{{ID: "site-long-generated-code", Name: "Example Clinic", Proxy: config.Proxy{Domain: "clinic.example.com"}}}
	m.snapshot.Detail = security.Report{Findings: []security.Finding{
		{SiteID: "site-long-generated-code", SiteIDs: []string{"base", "www"}, Container: "example-clinic", State: model.Warn, Title: "Memory limit"},
		{SiteID: "site-long-generated-code", SiteIDs: []string{"base", "www"}, Container: "example-clinic", State: model.Warn, Title: "Configured runtime user"},
	}}
	text := ansi.Strip(strings.Join(m.detailLines(110), "\n"))
	if strings.Contains(text, "site-long-generated-code") || strings.Count(text, "clinic.example.com") != 1 || strings.Contains(text, "Example Clinic") || !strings.Contains(text, "2 endpoints") || !strings.Contains(text, "Memory limit") {
		t.Fatal("security repeated IDs instead of labeling the shared runtime", text)
	}
}

func TestWWWPrecedesOtherChildrenAndNestedWWWPairsRemainAdjacent(t *testing.T) {
	sites := []config.Site{}
	for _, domain := range []string{"www.analytics.example.com", "analytics.example.com", "www.example.com", "example.com", "api.example.com"} {
		sites = append(sites, config.Site{ID: domain, Name: domain, Proxy: config.Proxy{Domain: domain}})
	}
	groups := groupSites(sites)
	if len(groups) != 1 {
		t.Fatal(groups)
	}
	for i, domain := range []string{"example.com", "www.example.com", "analytics.example.com", "www.analytics.example.com", "api.example.com"} {
		if groups[0].domains()[i].Proxy.Domain != domain {
			t.Fatal("www rows are separated", groups[0].domains())
		}
	}
}

func TestExplicitWWWNameDoesNotReplaceGroupedDomain(t *testing.T) {
	sites := []config.Site{
		{ID: "auto-apex", Name: "example.com", Proxy: config.Proxy{Domain: "example.com"}},
		{ID: "manual-www", Name: "Friendly property", Proxy: config.Proxy{Domain: "www.example.com"}},
		{ID: "analytics", Name: "Analytics service", Proxy: config.Proxy{Domain: "analytics.example.com"}},
	}
	groups := groupSites(sites)
	if len(groups) != 1 || groups[0].Primary.Name != "example.com" || groups[0].Primary.ID != "auto-apex" || sites[0].Name != "example.com" {
		t.Fatal("property lost its explicit name or changed persistent identity", groups)
	}
}
