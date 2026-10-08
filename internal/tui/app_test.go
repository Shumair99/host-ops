package tui

import (
	"bytes"
	tea "charm.land/bubbletea/v2"
	"context"
	"errors"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"sync"
	"testing"
	"time"
)

func testDashboard(t *testing.T) dashboard {
	t.Helper()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	c := DemoConfig()
	if err := SeedDemo(context.Background(), c, store, false); err != nil {
		t.Fatal(err)
	}
	m := newDashboard(context.Background(), Source{Config: c, Store: store, Demo: true}, Options{Interval: 60 * time.Second})
	m.snapshot, err = m.source.Base(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.loading = false
	return m
}
func press(m dashboard, key string) (dashboard, tea.Cmd) {
	code := rune(0)
	switch key {
	case "enter":
		code = tea.KeyEnter
	case "esc":
		code = tea.KeyEscape
	case "down":
		code = tea.KeyDown
	case "up":
		code = tea.KeyUp
	case "left":
		code = tea.KeyLeft
	case "right":
		code = tea.KeyRight
	case "tab":
		code = tea.KeyTab
	case "f5":
		code = tea.KeyF5
	default:
		if len(key) == 1 {
			code = rune(key[0])
		}
	}
	v, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: key})
	return v.(dashboard), cmd
}
func finish(m dashboard, cmd tea.Cmd) dashboard {
	if cmd == nil {
		return m
	}
	msg := cmd()
	v, _ := m.Update(msg)
	return v.(dashboard)
}

func TestASCIIFrameFitsResizedTerminals(t *testing.T) {
	m := testDashboard(t)
	for _, size := range [][2]int{{100, 30}, {80, 24}, {60, 16}, {140, 40}} {
		m.width, m.height = size[0], size[1]
		for _, item := range allViews() {
			m.page = item.Page
			m.loading = false
			m.lines = HelpLines()
			frame := m.frame()
			rows := strings.Split(frame, "\n")
			if len(rows) != m.height {
				t.Fatalf("%s rows=%d want=%d", item.Page, len(rows), m.height)
			}
			for _, row := range rows {
				if len(row) > m.width {
					t.Fatalf("%s width=%d want<=%d", item.Page, len(row), m.width)
				}
				for _, r := range row {
					if r > 127 || r == 27 {
						t.Fatal("frame contains non-ASCII or escape control")
					}
				}
			}
		}
	}
}
func TestSiteSelectionDrillDownAndFilter(t *testing.T) {
	m := testDashboard(t)
	m, cmd := press(m, "s")
	m = finish(m, cmd)
	m, _ = press(m, "down")
	m, cmd = press(m, "enter")
	m = finish(m, cmd)
	if m.page != "site" || m.siteID != "bravo" || !strings.Contains(strings.Join(m.lines, "\n"), "SITE PASSPORT") {
		t.Fatal(m.page, m.siteID, m.lines)
	}
	m, cmd = press(m, "esc")
	m = finish(m, cmd)
	m, _ = press(m, "/")
	m.input = "alpha"
	m, cmd = press(m, "enter")
	m = finish(m, cmd)
	if len(m.filteredSites()) != 1 || m.filteredSites()[0].ID != "alpha" {
		t.Fatal(m.filteredSites())
	}
}

func TestMenuPreviewsVerticallyAndRightOnlyChangesFocus(t *testing.T) {
	m := testDashboard(t)
	if !m.menuFocus {
		t.Fatal("dashboard did not start with menu focus")
	}
	m, cmd := press(m, "down")
	if cmd == nil || m.page != "sites" || !m.menuFocus || menu[m.navSelected].Page != "sites" {
		t.Fatal("moving the menu cursor did not preview the selected view", m.page, m.navSelected)
	}
	m = finish(m, cmd)
	m, cmd = press(m, "right")
	m = finish(m, cmd)
	if m.page != "sites" || m.menuFocus {
		t.Fatal("Right did not open the selected view")
	}
	m, _ = press(m, "down")
	if m.selected != 1 || menu[m.navSelected].Page != "sites" {
		t.Fatal("content arrows moved the menu instead of selecting a site")
	}
	m, _ = press(m, "left")
	m, _ = press(m, "down")
	if !m.menuFocus || m.page != "issues" || menu[m.navSelected].Page != "issues" {
		t.Fatal("menu selection did not update the preview")
	}
	m, cmd = press(m, "enter")
	m = finish(m, cmd)
	if m.page != "issues" || m.menuFocus {
		t.Fatal("Enter did not open the menu selection")
	}
	m, _ = press(m, "tab")
	if !m.menuFocus || m.page != "issues" {
		t.Fatal("Tab changed the view rather than the focused pane")
	}
}

func TestBackgroundSiteRefreshKeepsEvidenceVisibleAndDoesNotOverlap(t *testing.T) {
	m := testDashboard(t)
	m.siteID = "alpha"
	m.page = "site"
	var err error
	m.lines, err = m.source.Page(m.ctx, "site", "alpha", m.target, m.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	before := m.frame()
	cmd := m.reload()
	if m.loading || !m.reading || m.frame() != before || strings.Contains(m.frame(), "Reading site evidence") {
		t.Fatal("background refresh replaced the inspector with a loading screen")
	}
	generation := m.generation
	next, _ := m.Update(tickMsg{})
	if next.(dashboard).generation != generation {
		t.Fatal("another background read started while one was pending")
	}
	m = finish(m, cmd)
	if m.reading || m.loading || !strings.Contains(m.frame(), "SITE PASSPORT") {
		t.Fatal("completed background refresh did not retain the inspector")
	}
}

func TestMenuPreviewDoesNotRunDiskInspectionAndReusesEvidence(t *testing.T) {
	m := testDashboard(t)
	m.source.Demo = false
	index := menuIndex("disk")
	cmd := m.previewMenu(index)
	if cmd != nil || m.page != "disk" || !m.menuFocus || !strings.Contains(m.frame(), "ON-DEMAND INSPECTION") {
		t.Fatal("browsing Disk started a remote scan or failed to preview it")
	}
	m, cmd = press(m, "right")
	if cmd == nil || m.menuFocus || !m.loading {
		t.Fatal("entering a cold inspection did not start its read")
	}
	v, _ := m.Update(loadedMsg{generation: m.generation, snapshot: m.snapshot, lines: []string{"RETAINED DISK ACCOUNTING"}})
	m = v.(dashboard)
	m.previewMenu(menuIndex("overview"))
	cmd = m.previewMenu(index)
	if cmd != nil || !strings.Contains(m.frame(), "RETAINED DISK ACCOUNTING") {
		t.Fatal("revisiting an inspection discarded its evidence or ran another scan")
	}
}

func TestToolsKeepsOptionalViewsOutOfMainMenu(t *testing.T) {
	m := testDashboard(t)
	for _, item := range menu {
		if item.Page == "costs" || item.Page == "preflight" || item.Page == "deployments" {
			t.Fatal("optional view remains in the main menu")
		}
	}
	m, cmd := press(m, "a")
	m = finish(m, cmd)
	if m.page != "tools" || m.menuFocus {
		t.Fatal("Tools shortcut did not open its content")
	}
	m, cmd = press(m, "enter")
	m = finish(m, cmd)
	if m.page != toolMenu[0].Page {
		t.Fatal("Tools selection did not open the selected tool")
	}
	m, _ = press(m, "left")
	if !m.menuFocus || menu[m.navSelected].Page != "tools" {
		t.Fatal("Left from an optional tool did not focus its menu parent")
	}
}

func TestFailedBackgroundReadPreservesEvidenceAndRecovers(t *testing.T) {
	m := testDashboard(t)
	m.siteID, m.page = "alpha", "site"
	var err error
	m.lines, err = m.source.Page(m.ctx, "site", "alpha", m.target, m.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Join(m.lines, "\n")
	m.reload()
	v, _ := m.Update(loadedMsg{generation: m.generation, err: errors.New("read unavailable")})
	m = v.(dashboard)
	if m.reading || m.loading || strings.Join(m.lines, "\n") != before || !strings.Contains(m.notice, "showing previous evidence") {
		t.Fatal("failed background read discarded evidence or concealed the failure")
	}
	cmd := m.reload()
	m = finish(m, cmd)
	if m.notice != "" || !strings.Contains(m.frame(), "SITE PASSPORT") {
		t.Fatal("successful refresh retained the previous failure notice")
	}
}

func TestStalePageResultCannotReplaceNewView(t *testing.T) {
	m := testDashboard(t)
	m.generation = 4
	v, _ := m.Update(loadedMsg{generation: 3, lines: []string{"wrong page"}})
	m = v.(dashboard)
	if strings.Contains(strings.Join(m.lines, "\n"), "wrong page") {
		t.Fatal("stale asynchronous result was applied")
	}
}
func TestRefreshDoesNotOverlapAndOldTimersAreIgnored(t *testing.T) {
	m := newDashboard(context.Background(), Source{}, Options{Interval: time.Minute})
	if !m.collecting {
		t.Fatal("initial collection not marked running")
	}
	m, cmd := press(m, "f5")
	if cmd != nil {
		t.Fatal("second cycle started while initial collection was running")
	}
	m.collecting = false
	m.collectVersion = 2
	v, cmd := m.Update(collectTickMsg{version: 1})
	if cmd != nil || v.(dashboard).collecting {
		t.Fatal("obsolete timer started a cycle")
	}
	v, cmd = m.Update(collectTickMsg{version: 2})
	if cmd == nil || !v.(dashboard).collecting {
		t.Fatal("valid timer did not start collection")
	}
}
func TestDemoOutageAndRecoveryAreRecorded(t *testing.T) {
	m := testDashboard(t)
	m, cmd := press(m, "t")
	unchanged, second := press(m, "t")
	if second != nil || !unchanged.demoOutage {
		t.Fatal("a second demo scenario overlapped the first")
	}
	m = finish(m, cmd)
	records, err := m.source.Store.LatestEntities(m.ctx, m.source.Config.Host.ID, "incident")
	if err != nil || len(records) != 1 || !strings.Contains(string(records[0].Data), `"status":"open"`) {
		t.Fatal(records, err)
	}
	m, cmd = press(m, "t")
	m = finish(m, cmd)
	records, _ = m.source.Store.LatestEntities(m.ctx, m.source.Config.Host.ID, "incident")
	if len(records) != 1 || !strings.Contains(string(records[0].Data), `"status":"resolved"`) {
		t.Fatal(records)
	}
}
func TestFleetRowsStayTogetherAndHeadersCannotInjectRows(t *testing.T) {
	m := testDashboard(t)
	for _, width := range []int{60, 80, 100} {
		m.width = width
		for _, page := range []string{"sites", "overview"} {
			m.page = page
			for _, line := range m.liveLines() {
				if strings.Contains(line, "Example alpha") || strings.Contains(line, "Example bravo") {
					if len(line) > width-25 {
						t.Fatalf("fleet row wraps at width %d: %q", width, line)
					}
				}
			}
		}
	}
	m.source.Config.Host.ID = "host\n\tINJECTED\x1b[2J"
	if len(strings.Split(m.frame(), "\n")) != m.height {
		t.Fatal("manifest text injected terminal rows")
	}
}
func TestDashboardRejectsUnknownViewBeforeCollection(t *testing.T) {
	err := Run(context.Background(), config.Config{}, "", Options{Demo: true, Once: true, Page: "wrong"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown dashboard view") {
		t.Fatal(err)
	}
}
func TestStaleEvidenceCannotShowPassingLayerSymbols(t *testing.T) {
	m := testDashboard(t)
	m.source.Demo = false
	m.source.MaxAge = time.Nanosecond
	snapshot, err := m.source.Base(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range snapshot.Reports {
		if report.Overall != "unknown" || strings.Contains(chain(report), "+") {
			t.Fatal("stale health presented as passed", report)
		}
	}
	if len(snapshot.Host.Values) != 0 {
		t.Fatal("stale host measurements presented as current")
	}
}
func TestOfflineDemoEveryViewAvoidsRealInspection(t *testing.T) {
	m := testDashboard(t)
	for _, item := range allViews() {
		if _, err := m.source.Page(m.ctx, item.Page, "", "caddy", m.snapshot); err != nil {
			t.Fatalf("%s: %v", item.Page, err)
		}
	}
}
func TestOneShotDoesNotNeedTerminalTokenOrManifest(t *testing.T) {
	var out bytes.Buffer
	if err := Run(context.Background(), config.Config{}, "", Options{Demo: true, Once: true}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "DEMO / FICTIONAL DATA") || !strings.Contains(out.String(), "88.0%") {
		t.Fatal(out.String())
	}
}
func TestJobShutdownWaitsForActiveReadAndRejectsNewWork(t *testing.T) {
	j := &jobs{}
	started, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); j.command(func() tea.Msg { close(started); <-release; return nil })() }()
	<-started
	go func() { j.close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("shutdown did not wait")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-closed
	wg.Wait()
	ran := false
	j.command(func() tea.Msg { ran = true; return nil })()
	if ran {
		t.Fatal("new work started after shutdown")
	}
}
