package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/charmbracelet/x/ansi"
)

func relatedDomainDashboard(m dashboard) dashboard {
	child := m.snapshot.Sites[0]
	child.ID, child.Name = "example-site-a-analytics", "Example analytics"
	child.Proxy.Domain, child.Runtime.Container = "analytics.site-a.example", "demo-analytics"
	child.Proxy.Upstream = child.Runtime.Container + ":80"
	child.Health.PublicURL = "https://" + child.Proxy.Domain + "/healthz"
	m.snapshot.Sites = append(m.snapshot.Sites, child)
	m.source.Config.Sites = append(m.source.Config.Sites, child)
	r := m.snapshot.Reports[0]
	r.SiteID, r.Name, r.Runtime.Name = child.ID, child.Name, child.Runtime.Container
	r.Runtime.SiteID, r.Runtime.Image = child.ID, "demo:analytics"
	r.Checks = nil
	r.Diagnoses = nil
	for _, check := range m.snapshot.Reports[0].Checks {
		if check.Layer == "registration" {
			continue
		}
		check.SiteID, check.State, check.Code = child.ID, model.Pass, ""
		check.Summary, check.Details = "Fictional successful related-domain observation", nil
		if check.Layer == "local_http" || check.Layer == "public_http" {
			check.StatusCode, check.LatencyMS = 200, 35
		}
		r.Checks = append(r.Checks, check)
	}
	r.Overall = model.Overall(r.Checks)
	m.snapshot.Reports = append(m.snapshot.Reports, r)
	m.snapshot.Workloads[child.ID] = metrics.Sample{SiteID: child.ID, ObservedAt: time.Now(), Values: map[string]float64{"cpu_percent": 31, "memory_used_bytes": 77e6}}
	m.snapshot.SiteHistory = nil
	return m
}

func TestSubdomainPropertyKeepsIndependentHealthResourcesAndNavigation(t *testing.T) {
	m := relatedDomainDashboard(testDashboard(t))
	child := m.snapshot.Sites[len(m.snapshot.Sites)-1]
	index := len(m.snapshot.Reports) - 1
	for i := range m.snapshot.Reports[index].Checks {
		if m.snapshot.Reports[index].Checks[i].Layer == "public_http" {
			m.snapshot.Reports[index].Checks[i].State = model.Fail
			m.snapshot.Reports[index].Checks[i].StatusCode = 503
		}
	}
	if len(m.sites()) != 6 || m.report("example-site-a").Overall != "critical" || domainReport(m.snapshot.Sites[0], reportsByID(m.snapshot.Reports)).Overall != "healthy" {
		t.Fatal("grouping duplicated a property or changed independent health")
	}
	matrix := ansi.Strip(strings.Join(m.healthMatrix(122, 8), "\n"))
	if strings.Contains(matrix, child.Proxy.Domain) || !strings.Contains(matrix, "site-a.example +1") || !strings.Contains(matrix, "CRITICAL") {
		t.Fatal("subdomain escaped the property row or hid a failure", matrix)
	}
	ranking := ansi.Strip(strings.Join(m.pressureRanking(122, 20, "memory_used_bytes"), "\n"))
	if !strings.Contains(ranking, child.Proxy.Domain) || !strings.Contains(ranking, "73.4 MiB") || !strings.Contains(ranking, "41.7 MiB") {
		t.Fatal("parent grouping combined or omitted separate workloads", ranking)
	}
	m.filter = "analytics.site-a"
	if sites := m.filteredSites(); len(sites) != 1 || sites[0].ID != "example-site-a" {
		t.Fatal("filter no longer locates the child's parent", sites)
	}
	m.inputKind, m.input = "command", child.Proxy.Domain
	items := m.destinations()
	if len(items) != 1 || items[0].SiteID != child.ID {
		t.Fatal("subdomain no longer has a direct inspection destination", items)
	}
	if err := m.source.Store.Reports(m.ctx, m.source.Config.Host.ID, m.snapshot.Reports); err != nil {
		t.Fatal(err)
	}
	if _, err := m.source.Store.Add(m.ctx, m.source.Config.Host.ID, child.ID, "metrics", m.snapshot.Workloads[child.ID].ObservedAt, m.snapshot.Workloads[child.ID]); err != nil {
		t.Fatal(err)
	}
	m, cmd := press(m, "enter")
	m = finish(m, cmd)
	frame := ansi.Strip(m.frame())
	if m.siteID != child.ID || !strings.Contains(frame, child.Proxy.Domain) || !strings.Contains(frame, "demo-analytics") || !strings.Contains(frame, "31.0%") {
		t.Fatal("direct subdomain inspection borrowed the parent runtime or measurements", frame)
	}
	m.filter, m.siteSort = "", 2
	if sites := m.filteredSites(); sites[0].ID != "example-site-a" {
		t.Fatal("resource sort omitted the busy subdomain", sites)
	}
}

func TestPropertyGroupingRespectsSuffixBoundariesAndObservedParents(t *testing.T) {
	domains := []string{"analytics.example.co.uk", "example.co.uk", "metrics.tenant.github.io", "tenant.github.io", "other.github.io", "github.io", "api.lonely.example", "www.lonely.example", "192.0.2.1"}
	sites := []config.Site{}
	for _, d := range domains {
		sites = append(sites, config.Site{ID: d, Name: d, Proxy: config.Proxy{Domain: d}})
	}
	groups := groupSites(sites)
	if len(groups) != 7 {
		t.Fatal("unrelated tenants or unobserved parent domains were merged", groups)
	}
	for _, g := range groups {
		if len(g.Children) > 0 && g.Primary.ID != "example.co.uk" && g.Primary.ID != "tenant.github.io" {
			t.Fatal("property crossed its registrable domain boundary", g)
		}
	}
}

func TestResourceRowsDeduplicateOnlyTheSameWorkloadAndPreferItsNewestSample(t *testing.T) {
	m := relatedDomainDashboard(testDashboard(t))
	alias := m.snapshot.Sites[0]
	alias.ID, alias.Proxy.Domain = "example-site-a-www", "www.site-a.example"
	m.snapshot.Sites = append(m.snapshot.Sites, alias)
	m.snapshot.Workloads[alias.ID] = metrics.Sample{ObservedAt: time.Now().Add(time.Second), Values: map[string]float64{"cpu_percent": 19, "memory_used_bytes": 43.7e6}}
	readings := m.workloadReadings(groupFor(m.rawSites(), "example-site-a").domains())
	if len(readings) != 2 || readings[0].Sample.Values["cpu_percent"] != 19 || readings[0].Label != "site-a.example" || readings[1].Sample.Values["cpu_percent"] != 31 {
		t.Fatal("alias duplicated a workload or analytics lost its independent sample", readings)
	}
}

func TestInspectingSubdomainCannotReuseAnotherDomainsHistory(t *testing.T) {
	m := relatedDomainDashboard(testDashboard(t))
	if _, err := m.source.Evidence(m.ctx, "site", "example-site-a", "host", &m.snapshot); err != nil || len(m.snapshot.SiteHistory) == 0 {
		t.Fatal("primary-domain history unavailable", err)
	}
	child := m.snapshot.Sites[len(m.snapshot.Sites)-1]
	if summary := strings.Join(m.inspectorSummary(child, 122), "\n"); strings.Contains(summary, "Workload CPU") {
		t.Fatal("inspector reused old-domain history while the new view loads", summary)
	}
	if _, err := m.source.Evidence(m.ctx, "site", "example-site-a-analytics", "host", &m.snapshot); err != nil || len(m.snapshot.SiteHistory) != 0 {
		t.Fatal("unobserved subdomain inherited primary-domain history", err, m.snapshot.SiteHistory)
	}
}

func TestRedirectPropertyUsesServingRuntimeAndOmitsRedirectWorkload(t *testing.T) {
	m := testDashboard(t)
	base := m.snapshot.Sites[0]
	root := base
	root.ID, root.Name = "therapy-apex", "thetherapyspot.co.uk"
	root.Proxy.Domain, root.Proxy.Upstream = "thetherapyspot.co.uk", ""
	root.Proxy.RedirectTo = "www.thetherapyspot.co.uk"
	root.Runtime = config.Runtime{Type: "unresolved"}
	target := base
	target.ID, target.Name = "therapy-www", "www.thetherapyspot.co.uk"
	target.Proxy.Domain, target.Proxy.Upstream = "www.thetherapyspot.co.uk", "thetherapyspot:80"
	target.Proxy.RedirectTo = ""
	target.Runtime = config.Runtime{Type: "docker", Container: "thetherapyspot", Port: 80}
	m.snapshot.Sites = []config.Site{root, target}
	m.source.Config.Sites = []config.Site{root, target}
	rootReport := m.snapshot.Reports[0]
	rootReport.SiteID, rootReport.Name = root.ID, root.Name
	rootReport.Runtime = model.Runtime{SiteID: root.ID, Type: "unresolved", Status: "unknown"}
	targetReport := m.snapshot.Reports[0]
	targetReport.SiteID, targetReport.Name = target.ID, target.Name
	targetReport.Runtime = model.Runtime{SiteID: target.ID, Type: "docker", Name: "thetherapyspot", Status: "running", Health: "healthy", Image: "thetherapyspot:746c2cc", Commit: "746c2cc8c1eb2a6970611a43a3e6155d8c48c14f"}
	m.snapshot.Reports = []model.Report{rootReport, targetReport}
	now := time.Now()
	m.snapshot.Workloads = map[string]metrics.Sample{
		root.ID:   {SiteID: root.ID, ObservedAt: now, Values: map[string]float64{}},
		target.ID: {SiteID: target.ID, ObservedAt: now, Values: map[string]float64{"cpu_percent": 8.4, "memory_used_bytes": 8.57 * 1024 * 1024}},
	}

	group := groupFor(m.rawSites(), root.ID)
	if group.Primary.ID != target.ID || len(group.Children) != 1 || group.Children[0].ID != root.ID {
		t.Fatalf("confirmed redirect destination was not selected as the property runtime: %+v", group)
	}
	if sites := m.sites(); len(sites) != 1 || sites[0].ID != target.ID {
		t.Fatalf("property list did not use the serving address: %+v", sites)
	}
	readings := m.workloadReadings(group.domains())
	if len(readings) != 1 || readings[0].Label != target.Proxy.Domain || readings[0].Sample.Values["cpu_percent"] != 8.4 {
		t.Fatalf("redirect was listed as a separate workload or target sample was lost: %+v", readings)
	}
	details := ansi.Strip(strings.Join(m.siteDetails(root, 120, false), "\n"))
	for _, want := range []string{"docker / running", "thetherapyspot:80", "thetherapyspot:746c2cc", "746c2cc8c1eb2a6970611a43a3e6155d8c48c14f", "8.4%", "8.6 MiB", "Runtime source  www.thetherapyspot.co.uk"} {
		if !strings.Contains(details, want) {
			t.Fatalf("redirect inspection did not display destination runtime detail %q:\n%s", want, details)
		}
	}
	inspector := ansi.Strip(strings.Join(m.inspectorSummary(root, 120), "\n"))
	for _, want := range []string{"ROUTE  www.thetherapyspot.co.uk → thetherapyspot:80", "REDIRECT  thetherapyspot.co.uk → www.thetherapyspot.co.uk", "thetherapyspot:746c2cc", "746c2cc8c1eb2a6970611a43a3e6155d8c48c14f"} {
		if !strings.Contains(inspector, want) {
			t.Fatalf("property inspector did not resolve destination detail %q:\n%s", want, inspector)
		}
	}
}
