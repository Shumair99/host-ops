package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/charmbracelet/x/ansi"
)

func TestEveryEvidenceViewFitsWithAndWithoutColor(t *testing.T) {
	m := testDashboard(t)
	for _, colored := range []bool{true, false} {
		m.theme.color = colored
		for _, item := range allViews() {
			m.page = item.Page
			var err error
			m.lines, err = m.source.Evidence(m.ctx, item.Page, "", "host", &m.snapshot)
			if err != nil {
				t.Fatalf("%s: %v", item.Page, err)
			}
			for _, size := range [][2]int{{60, 16}, {80, 24}, {100, 30}, {120, 42}, {160, 48}} {
				m.width, m.height = size[0], size[1]
				frame := m.frame()
				if len(strings.Split(frame, "\n")) != m.height {
					t.Fatalf("%s: wrong row count", item.Page)
				}
				for _, line := range strings.Split(frame, "\n") {
					if ansi.StringWidth(line) > m.width {
						t.Fatalf("%s overflows %d cells: %q", item.Page, m.width, line)
					}
				}
				if !colored && strings.Contains(frame, "\x1b") {
					t.Fatal("no-color frame includes escapes")
				}
			}
		}
	}
}
func TestEvidenceCannotInjectTerminalControls(t *testing.T) {
	m := testDashboard(t)
	m.width = 120
	payload := "Injected\x1b[2J\x1b[31m\x1b]52;c;secret\a\nNew row"
	m.snapshot.Sites[0].Name = payload
	m.snapshot.Sites[0].Proxy.Domain = payload
	m.snapshot.Reports[0].Name = payload
	m.snapshot.Reports[0].Checks[0].Summary = payload
	m.source.Config.Host.Name = payload
	for _, page := range []string{"overview", "sites", "site", "inventory", "issues"} {
		m.page = page
		m.siteID = "alpha"
		frame := m.frame()
		if strings.Contains(frame, "\x1b[2J") || strings.Contains(frame, "\x1b]52") || strings.Contains(frame, "\a") {
			t.Fatalf("%s renders evidence controls", page)
		}
		if len(strings.Split(frame, "\n")) != m.height {
			t.Fatalf("%s allows evidence to inject rows", page)
		}
	}
}
func TestColorModesAndUnknownMeasurements(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := testDashboard(t)
	if m.theme.color {
		t.Fatal("NO_COLOR ignored")
	}
	m.snapshot.Host.Values = nil
	m.snapshot.History = nil
	text := strings.Join(m.resourcePanels(98), "\n")
	if strings.Contains(text, "0.0%") || strings.Contains(text, "█") || !strings.Contains(text, "—") {
		t.Fatal("missing metric was drawn as measured data", text)
	}
	m.snapshot.Host.Values = map[string]float64{"cpu_percent": 0}
	if !strings.Contains(strings.Join(m.resourcePanels(98), "\n"), "0.0%") {
		t.Fatal("a measured zero was lost")
	}
	m.theme.color = true
	if !strings.Contains(m.frame(), "\x1b[38;5;") {
		t.Fatal("colored mode has no semantic colors")
	}
}
func TestHistoryBucketsKeepGapsAndActualZeros(t *testing.T) {
	at := time.Now()
	samples := []metrics.Sample{{ObservedAt: at, Values: map[string]float64{"cpu_percent": 0}}, {ObservedAt: at.Add(4 * time.Minute), Values: map[string]float64{"cpu_percent": 40}}}
	points := timeBuckets(samples, "cpu_percent", 5)
	if points[0] == nil || *points[0] != 0 || points[4] == nil || *points[4] != 40 || points[1] != nil || points[2] != nil || points[3] != nil {
		t.Fatal("history fabricated points or lost a zero", points)
	}
}
func TestDiskKeepsCachedAccountingAndPartialMeaning(t *testing.T) {
	m := testDashboard(t)
	disk := DemoDisk()
	if err := m.source.Store.Transition(m.ctx, m.source.Config.Host.ID, "", "disk_inspection", disk, "", disk.ObservedAt, nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err := m.source.Base(m.ctx)
	if err != nil || snapshot.Disk == nil {
		t.Fatal("retained disk inspection unavailable", err)
	}
	m.snapshot = snapshot
	m.page = "disk"
	m.width = 120
	text := ansi.Strip(strings.Join(m.liveLines(), "\n"))
	if !strings.Contains(text, "≥") || !strings.Contains(text, "Build Cache") || !strings.Contains(text, "/var/log") || strings.Contains(text, "Browsing the menu") {
		t.Fatal("disk facts or partial meaning were lost", text)
	}
}

func TestDiskTelemetryRefreshPreservesInspectionAndCannotReplaceNewView(t *testing.T) {
	m := testDashboard(t)
	m.page = "disk"
	disk := DemoDisk()
	m.snapshot.Disk = &disk
	v, _ := m.Update(tickMsg{})
	m = v.(dashboard)
	if !m.telemetryReading || m.reading {
		t.Fatal("disk tick started a full inspection")
	}
	next := m.snapshot
	next.Host.Values = map[string]float64{"disk_percent": 89}
	next.Disk = nil
	v, _ = m.Update(telemetryMsg{generation: m.generation, snapshot: next})
	m = v.(dashboard)
	if m.snapshot.Disk == nil || m.snapshot.Host.Values["disk_percent"] != 89 {
		t.Fatal("telemetry refresh lost cached accounting or capacity")
	}
	oldGeneration := m.generation
	m.generation++
	m.page = "sites"
	next.Host.Values = map[string]float64{"disk_percent": 99}
	v, _ = m.Update(telemetryMsg{generation: oldGeneration, snapshot: next})
	m = v.(dashboard)
	if m.snapshot.Host.Values["disk_percent"] != 89 {
		t.Fatal("late disk telemetry replaced a newer view")
	}
}

func TestStaleMetricDecisionIsNotOverriddenByRetainedTimestamp(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.Host.ObservedAt = time.Now().Add(-10 * time.Minute)
	next := m.snapshot
	next.Host.ObservedAt = time.Now().Add(-20 * time.Minute)
	next.Host.Values = nil
	v, _ := m.Update(loadedMsg{generation: m.generation, snapshot: next})
	m = v.(dashboard)
	if len(m.snapshot.Host.Values) != 0 {
		t.Fatal("older cached metrics overrode the stale-data decision")
	}
}
