package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/charmbracelet/x/ansi"
)

func liveLoadingDashboard(t *testing.T) dashboard {
	t.Helper()
	m := testDashboard(t)
	m.source.Demo, m.source.Live, m.noCollect, m.collecting = false, true, true, false
	m.snapshot.Connection = agent.Connection{Connected: true, ObservedAt: time.Now()}
	return m
}

func TestPropagationHeaderCyclesDotsWithoutMovingAdjacentNotices(t *testing.T) {
	m := liveLoadingDashboard(t)
	m.startupComplete = false
	m.loading = true
	m.snapshot.HistoryNotice = "History was pruned"
	markerColumn := -1
	for _, dots := range []int{1, 2, 3, 1} {
		header := ansi.Strip(strings.Split(m.frame(), "\n")[1])
		want := "PROPAGATING" + strings.Repeat(".", dots)
		if !strings.Contains(header, want+strings.Repeat(" ", 3-dots)+"  HISTORY") || strings.Contains(header, "CONNECTED") {
			t.Fatal("incorrect loading animation", header)
		}
		column := strings.Index(header, "HISTORY")
		if markerColumn >= 0 && column != markerColumn {
			t.Fatal("dot animation moved neighboring text", header)
		}
		markerColumn = column
		v, _ := m.Update(tickMsg{})
		m = v.(dashboard)
	}
	for _, width := range []int{60, 100, 140} {
		m.width = width
		m.snapshot.HistoryNotice = ""
		for _, row := range strings.Split(m.frame(), "\n") {
			if ansi.StringWidth(row) > width {
				t.Fatal("loading header overflowed terminal", width, row)
			}
		}
	}
	m.snapshot.Host.Values = nil // Genuine missing values do not imply loading.
	v, _ := m.Update(loadedMsg{generation: m.generation, snapshot: m.snapshot})
	m = v.(dashboard)
	if m.propagating() || !strings.Contains(m.frame(), "CONNECTED") {
		t.Fatal("unknown data kept a completed page loading")
	}
}

func TestCompletedStartupNeverPropagatesForLaterWork(t *testing.T) {
	tests := map[string]func(*dashboard){
		"new page": func(m *dashboard) { m.goPage("readiness") },
		"collector checks": func(m *dashboard) {
			m.snapshot.Connection.Pending = []string{"public_checks", "inventory", "workload_metrics"}
		},
		"manual collection": func(m *dashboard) { m.collecting = true },
		"local read":        func(m *dashboard) { m.reading = true },
		"disk scan":         func(m *dashboard) { m.page, m.diskReading = "disk", true },
		"disk telemetry":    func(m *dashboard) { m.page, m.telemetryReading = "disk", true },
		"billing request":   func(m *dashboard) { m.settings = &settingsEditor{Busy: true} },
		"demo update":       func(m *dashboard) { m.demoBusy = true },
		"missing values":    func(m *dashboard) { m.snapshot.Host.Values = nil; m.snapshot.Workloads = nil },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			m := liveLoadingDashboard(t)
			change(&m)
			if m.propagating() || !strings.Contains(m.frame(), "CONNECTED") || strings.Contains(m.frame(), "PROPAGATING") {
				t.Fatal("later work restarted the startup animation")
			}
		})
	}
}

func TestInitialPropagationWaitsForCurrentSessionAndCompletesWithUnknownResults(t *testing.T) {
	m := liveLoadingDashboard(t)
	start := time.Now()
	m.source.LiveStartedAt = start
	m.startupComplete, m.loading = false, true
	m.snapshot = Snapshot{}
	if !m.propagating() {
		t.Fatal("empty initial dashboard did not propagate")
	}
	// A successful status retained from the previous run is not this run's load.
	v, _ := m.Update(loadedMsg{generation: m.generation, snapshot: Snapshot{LoadedAt: start, Connection: agent.Connection{Connected: true, ObservedAt: start.Add(-time.Minute)}}})
	m = v.(dashboard)
	if !m.propagating() {
		t.Fatal("retained connection status completed startup prematurely")
	}
	connection := agent.Connection{Connected: true, ObservedAt: start.Add(time.Second), Pending: []string{"host_metrics", "inventory", "workload_metrics", "public_checks"}}
	v, _ = m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	connection.ObservedAt = connection.ObservedAt.Add(time.Second)
	connection.Pending = []string{"public_checks"}
	v, _ = m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if !m.propagating() {
		t.Fatal("partial initial checks completed startup prematurely")
	}
	// No host values or workload readings are available, but their checks finished.
	connection.ObservedAt = connection.ObservedAt.Add(time.Second)
	connection.Pending = nil
	v, _ = m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if m.propagating() || !strings.Contains(m.frame(), "CONNECTED") {
		t.Fatal("completed unknown results kept startup loading")
	}
	// A new refresh can begin before another page snapshot arrives.
	connection.ObservedAt = connection.ObservedAt.Add(time.Second)
	connection.Pending = []string{"public_checks"}
	v, _ = m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if m.propagating() {
		t.Fatal("a subsequent collector refresh restarted propagation")
	}
}

func TestInitialPropagationWaitsForPageDataAfterChecksComplete(t *testing.T) {
	m := liveLoadingDashboard(t)
	m.startupComplete, m.loading = false, true
	m.snapshot = Snapshot{}
	connection := agent.Connection{Connected: true, ObservedAt: time.Now()}
	v, _ := m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if !m.propagating() {
		t.Fatal("completed checks concealed the initial page load")
	}
	v, _ = m.Update(loadedMsg{generation: m.generation, snapshot: Snapshot{LoadedAt: time.Now()}})
	m = v.(dashboard)
	if m.propagating() {
		t.Fatal("initial page data did not finish propagation")
	}
}

func TestLocalAndLegacyStartupFinishAfterInitialLoad(t *testing.T) {
	for _, collecting := range []bool{false, true} {
		m := testDashboard(t)
		m.startupComplete, m.loading, m.collecting = false, true, collecting
		v, _ := m.Update(loadedMsg{generation: m.generation, snapshot: m.snapshot})
		m = v.(dashboard)
		if m.propagating() != collecting {
			t.Fatal("initial load did not wait only for its first collection", collecting)
		}
		if collecting {
			v, _ = m.Update(cycleMsg{})
			m = v.(dashboard)
			if m.propagating() {
				t.Fatal("first observation cycle did not finish propagation")
			}
		}
	}
}

func TestDisconnectAndReconnectDoNotRestartPropagation(t *testing.T) {
	m := liveLoadingDashboard(t)
	connection := agent.Connection{ObservedAt: time.Now(), Issue: "Collector disconnected"}
	v, _ := m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if m.propagating() || !strings.Contains(m.frame(), "CONNECTING") {
		t.Fatal("disconnection was presented as initial propagation")
	}
	connection.Connected, connection.Pending, connection.Issue = true, []string{"inventory"}, ""
	connection.ObservedAt = connection.ObservedAt.Add(time.Second)
	v, _ = m.Update(connectionMsg{connection: connection})
	m = v.(dashboard)
	if m.propagating() || !strings.Contains(m.frame(), "CONNECTED") {
		t.Fatal("reconnection restarted the startup animation")
	}
}

func TestCachedToolViewReadsCompletedProgressWithoutRerunningAssessment(t *testing.T) {
	m := liveLoadingDashboard(t)
	m.startupComplete = false
	m.page = "readiness"
	m.lines = []string{"Retained assessment"}
	m.snapshot.Connection.Pending = []string{"public_checks"}
	completed := agent.Connection{Connected: true, ObservedAt: m.snapshot.Connection.ObservedAt.Add(time.Second)}
	if err := m.source.Store.Transition(m.ctx, m.source.Config.Host.ID, "", "connection", completed, "", completed.ObservedAt, nil); err != nil {
		t.Fatal(err)
	}
	v, _ := m.Update(tickMsg{})
	m = v.(dashboard)
	if !m.connectionReading || m.reading || m.loading {
		t.Fatal("status poll started a fresh assessment")
	}
	m = finish(m, m.connectionCmd())
	if m.connectionReading || m.propagating() || !strings.Contains(m.frame(), "CONNECTED") || len(m.lines) != 1 {
		t.Fatal("completed collector checks stayed pending on a cached tool")
	}
	old := completed
	old.ObservedAt, old.Pending = old.ObservedAt.Add(-time.Second), []string{"public_checks"}
	v, _ = m.Update(loadedMsg{generation: m.generation, snapshot: Snapshot{Connection: old}})
	m = v.(dashboard)
	if len(m.snapshot.Connection.Pending) != 0 {
		t.Fatal("a slow page response restored stale loading status")
	}
}

func TestFailedPageReadStopsPropagationAndCollectionErrorsRemainVisible(t *testing.T) {
	m := liveLoadingDashboard(t)
	m.startupComplete = false
	m.loading, m.reading = true, true
	v, _ := m.Update(loadedMsg{generation: m.generation, err: errors.New("Read unavailable")})
	m = v.(dashboard)
	if m.propagating() || !strings.Contains(m.notice, "Read failed") {
		t.Fatal("failed read still pretended to be loading")
	}
	m.loading, m.collectErr = true, "Collection failed"
	header := ansi.Strip(strings.Split(m.frame(), "\n")[1])
	if !strings.Contains(header, "COLLECTION ERROR") || strings.Contains(header, "PROPAGATING") {
		t.Fatal("loading indicator concealed a collection error", header)
	}
}
