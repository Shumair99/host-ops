package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

func aliasFixture() (config.Config, Snapshot) {
	sites := []config.Site{
		{ID: "www", Name: "www.example.com", Proxy: config.Proxy{Domain: "www.example.com"}},
		{ID: "base", Name: "example.com", Proxy: config.Proxy{Domain: "example.com"}},
		{ID: "standalone", Name: "www.other.example", Proxy: config.Proxy{Domain: "www.other.example"}},
	}
	reports := []model.Report{}
	for _, site := range sites {
		r := model.Report{SiteID: site.ID, Overall: "healthy", ObservedAt: time.Now()}
		for _, layer := range []string{"runtime", "local_http", "proxy_route", "dns", "tls", "public_http"} {
			r.Checks = append(r.Checks, model.Check{Layer: layer, State: model.Pass, Summary: "Passed"})
		}
		if site.ID == "www" {
			r.Overall = "critical"
			r.Checks[4].State = model.Fail
			r.Checks[4].Summary = "WWW certificate failure"
		}
		reports = append(reports, r)
	}
	return config.Config{Sites: sites}, Snapshot{Sites: sites, Reports: reports}
}

func TestFleetGroupsWWWAndKeepsAliasFailureInInspector(t *testing.T) {
	c, snapshot := aliasFixture()
	m := newDashboard(context.Background(), Source{Config: c}, Options{})
	m.snapshot, m.loading = snapshot, false
	for _, page := range []string{"overview", "sites"} {
		m.page = page
		lines := strings.Join(m.liveLines(), "\n")
		if len(m.sites()) != 2 || strings.Contains(lines, "www.example.com") || !strings.Contains(lines, "CRITICAL") {
			t.Fatal("alias was duplicated or its failure was hidden", lines)
		}
	}
	m.filter = "www.example.com"
	if sites := m.filteredSites(); len(sites) != 1 || sites[0].ID != "base" {
		t.Fatal("alias filtering did not find its grouped site", sites)
	}
	combined := m.report("base")
	if combined.Overall != "critical" || !strings.Contains(chain(combined), "[!]") {
		t.Fatal("grouped health strip concealed the alias failure")
	}
	lines, err := (Source{Config: c}).Page(context.Background(), "site", "base", "", snapshot)
	if err != nil || !strings.Contains(strings.Join(lines, "\n"), "www.example.com") || !strings.Contains(strings.Join(lines, "\n"), "WWW certificate failure") {
		t.Fatal("inspector did not retain independent alias evidence", err, lines)
	}
	if len(snapshot.Sites) != 3 || snapshot.Reports[1].Overall != "healthy" {
		t.Fatal("presentation grouping mutated monitoring facts")
	}
}

func TestUnobservedAliasPreventsGroupedPassingLayers(t *testing.T) {
	c, snapshot := aliasFixture()
	snapshot.Reports = snapshot.Reports[1:]
	m := newDashboard(context.Background(), Source{Config: c}, Options{})
	m.snapshot = snapshot
	report := m.report("base")
	if report.Overall != "unknown" || strings.Contains(chain(report), "[+]") {
		t.Fatal("missing alias observations became a passing grouped site", report)
	}
	// Source.Base retains an unknown report with only a collector check when a
	// discovered domain has not yet received its first observations.
	snapshot.Reports = append(snapshot.Reports, model.Report{SiteID: "www", Overall: "unknown", Checks: []model.Check{{Layer: "collector", State: model.Unknown}}})
	m.snapshot = snapshot
	report = m.report("base")
	if report.Overall != "unknown" || strings.Contains(chain(report), "[+]") {
		t.Fatal("partial alias evidence inherited the primary domain's passing layers", report)
	}
}

func TestGroupingOnlyPairsObservedWWWWithItsExactBase(t *testing.T) {
	groups := groupSites([]config.Site{{ID: "a", Proxy: config.Proxy{Domain: "EXAMPLE.com."}}, {ID: "w", Proxy: config.Proxy{Domain: "www.example.com"}}, {ID: "api", Proxy: config.Proxy{Domain: "api.example.com"}}, {ID: "lonely", Proxy: config.Proxy{Domain: "www.elsewhere.com"}}})
	if len(groups) != 3 {
		t.Fatal("unrelated domains were merged")
	}
	paired := 0
	for _, group := range groups {
		paired += len(group.Aliases)
	}
	if paired != 1 {
		t.Fatal("exact observed WWW/base pair did not group", groups)
	}
}
