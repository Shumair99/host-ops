package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/x/ansi"
)

func TestJumpNavigatorOpensViewsAndGroupedPropertiesAndRestoresOnCancel(t *testing.T) {
	m := testDashboard(t)
	m.page = "host"
	m.offset = 10
	v, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	m = v.(dashboard)
	if cmd != nil || m.inputKind != "command" || !strings.Contains(m.frame(), "Jump to") {
		t.Fatal("command search did not open in place")
	}
	m.input = "pressure"
	items := m.destinations()
	if len(items) != 1 || items[0].Page != "host" {
		t.Fatal("view descriptions are not searchable", items)
	}
	m, _ = press(m, "esc")
	if m.page != "host" || m.offset != 10 || m.inputKind != "" {
		t.Fatal("cancel changed the active workspace")
	}
	m.inputKind = "command"
	m.input = "site-b.example.invalid"
	m, cmd = press(m, "enter")
	m = finish(m, cmd)
	if m.page != "site" || m.siteID != "example-site-b" || m.menuFocus {
		t.Fatal("property jump did not inspect its evidence")
	}
	m.inputKind = "command"
	m.input = "does-not-exist"
	m, cmd = press(m, "enter")
	if cmd != nil || m.page != "site" {
		t.Fatal("an empty search changed the current view")
	}
}

func TestNetworkRateRejectsRebootCounterResetChangedInterfacesAndGaps(t *testing.T) {
	at := time.Now().Add(-30 * time.Second)
	base := func(at time.Time, rx, uptime, identity float64) metrics.Sample {
		return metrics.Sample{ObservedAt: at, Values: map[string]float64{"network_rx_bytes": rx, "network_tx_bytes": rx, "network_interfaces": 1, "network_interface_identity": identity, "uptime_seconds": uptime}}
	}
	a := base(at, 100, 900, 12)
	b := base(at.Add(10*time.Second), 350, 910, 12)
	rates := networkRates([]metrics.Sample{a, b})
	if rates[1].Values["network_rx_rate"] != 25 {
		t.Fatal("rate is not bytes per elapsed second", rates)
	}
	for _, bad := range []metrics.Sample{base(b.ObservedAt, 50, 910, 12), base(b.ObservedAt, 350, 1, 12), base(b.ObservedAt, 350, 910, 13), base(at.Add(4*time.Minute), 350, 1140, 12), base(at, 350, 910, 12), {ObservedAt: b.ObservedAt, Values: nil}} {
		if got := networkRates([]metrics.Sample{a, bad}); len(got[1].Values) > 0 {
			t.Fatal("invalid cumulative difference became a rate", got)
		}
	}
	b = base(b.ObservedAt, 100, 910, 12)
	if n, ok := networkRates([]metrics.Sample{a, b})[1].Values["network_rx_rate"]; !ok || n != 0 {
		t.Fatal("measured idle traffic became unavailable")
	}
}

func TestSitesSortMissingMetricsAfterMeasuredValuesAndDoesNotLoseAliasFailures(t *testing.T) {
	m := testDashboard(t)
	m.siteSort = 2
	m.snapshot.Workloads["example-site-a"] = metrics.Sample{Values: nil}
	sites := m.filteredSites()
	if sites[len(sites)-1].ID != "example-site-a" {
		t.Fatal("missing CPU sorted as measured zero")
	}
	m.snapshot.Reports[1].Checks[0].State = model.Fail
	m.snapshot.Reports[1].Overall = "critical"
	m.siteSort = 1
	if m.filteredSites()[0].ID != "example-site-b" {
		t.Fatal("critical property did not sort first")
	}
}

func TestObservationMapHasNoContinuousUptimeClaimAndSanitizesEvidence(t *testing.T) {
	m := testDashboard(t)
	m.width = 140
	data, _ := json.Marshal(model.Report{SiteID: "example-site-a", Overall: "critical", Name: "unsafe\x1b]52;c;secret\a"})
	feed := recordFeed{Page: "monitoring", Records: []storage.Record{{Kind: "health", SiteID: "example-site-a", ObservedAt: time.Now().Add(-time.Hour), Data: data}}}
	text := strings.Join(m.recordWorkspace(feed, 118), "\n")
	if !strings.Contains(text, "· no record") || !strings.Contains(text, "!") || strings.Contains(text, "uptime") || strings.Contains(text, "\x1b]52") {
		t.Fatal("observation evidence was misrepresented", text)
	}
	m.theme.color = false
	text = strings.Join(m.recordWorkspace(feed, 118), "\n")
	if strings.Contains(text, "\x1b") {
		t.Fatal("monochrome charts emitted escapes")
	}
}

func TestAssessmentSelectionWindowsTheListAndKeepsOnlySelectedDetails(t *testing.T) {
	m := testDashboard(t)
	m.page = "readiness"
	m.menuFocus = false
	m.width, m.height = 120, 36
	assessments := []readiness.Assessment{}
	for i := 0; i < 30; i++ {
		assessments = append(assessments, readiness.Assessment{Name: fmt.Sprintf("property-%02d", i), Provisional: true, MaxScore: 10})
	}
	m.snapshot.Detail = assessments
	m.assessmentSelected = 28
	m, _ = press(m, "down")
	if m.assessmentSelected != 29 {
		t.Fatal("assessment arrows did not select the next property")
	}
	text := ansi.Strip(strings.Join(m.liveLines(), "\n"))
	if !strings.Contains(text, "property-29") || strings.Contains(text, "property-00") {
		t.Fatal("large assessment list buried the selected property", text)
	}
	if strings.Count(text, "passed rules ·") != 1 {
		t.Fatal("detail blocks repeat for unselected properties")
	}
	m, _ = press(m, "1")
	if !m.showPassing {
		t.Fatal("passing-evidence toggle did not respond")
	}
}

// Export the real rendered frames for optional visual review, using only the
// isolated fictional demo. Normal test runs do not write workspace artifacts.
func TestFrontendPreviewExport(t *testing.T) {
	dir := os.Getenv("HOSTOPS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set HOSTOPS_PREVIEW_DIR to render review frames")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := testDashboard(t)
	m.width, m.height = 144, 44
	m.theme.color = true
	m.menuFocus = false
	for _, page := range append(allViews(), menuItem{Page: "site"}, menuItem{Page: "logs"}) {
		m.page = page.Page
		m.siteID = ""
		m.snapshot.Detail = nil
		if page.Page == "site" || page.Page == "logs" {
			m.siteID = "example-site-b"
		}
		var err error
		m.lines, err = m.source.Evidence(m.ctx, page.Page, m.siteID, "host", &m.snapshot)
		if err != nil {
			t.Fatal(page.Page, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "demo-"+page.Page+".txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "full-"+page.Page+".txt"), []byte(strings.Join(m.liveLines(), "\n")), 0600); err != nil {
			t.Fatal(err)
		}
		for _, line := range m.liveLines() {
			if ansi.StringWidth(line) > m.bodyWidth() {
				t.Fatalf("%s live content overflows: %q", page.Page, line)
			}
		}
	}
	m.page = "overview"
	m.siteID = ""
	m.inputKind = "command"
	m.input = ""
	m.commandSelected = 4
	if err := os.WriteFile(filepath.Join(dir, "demo-jump.txt"), []byte(m.frame()), 0600); err != nil {
		t.Fatal(err)
	}
	m.inputKind, m.input = "", ""
	m, cmd := press(m, "t")
	m = finish(m, cmd)
	m = finish(m, m.reload()) // Complete the read scheduled after the scenario cycle.
	for _, page := range []string{"overview", "issues", "incidents", "site"} {
		m.page = page
		m.siteID = ""
		if page == "site" {
			m.siteID = "example-site-b"
		}
		var err error
		m.lines, err = m.source.Evidence(m.ctx, page, m.siteID, "host", &m.snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "demo-outage-"+page+".txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.page = "overview"
	m.siteID = ""
	m.width, m.height = 80, 24
	if err := os.WriteFile(filepath.Join(dir, "demo-narrow.txt"), []byte(m.frame()), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ansi.Strip(m.frame()), "CRITICAL") {
		t.Fatal("narrow overview hid the active failure below resource charts")
	}
	m = fictionalDNSFailure(m)
	m.width, m.height = 144, 44
	for _, page := range []string{"overview", "issues", "site"} {
		m.page, m.siteID = page, ""
		if page == "site" {
			m.siteID = "example-site-a"
		}
		if err := os.WriteFile(filepath.Join(dir, "demo-dns-"+page+".txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "full-dns-"+page+".txt"), []byte(strings.Join(m.liveLines(), "\n")), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m = relatedDomainDashboard(m)
	for _, page := range []string{"overview", "sites", "site"} {
		m.page, m.siteID, m.offset = page, "", 0
		if page == "site" {
			m.siteID = "example-site-a-analytics"
		}
		if err := os.WriteFile(filepath.Join(dir, "demo-subdomains-"+page+".txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "full-subdomains-"+page+".txt"), []byte(strings.Join(m.liveLines(), "\n")), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
