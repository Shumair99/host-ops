package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/charmbracelet/x/ansi"
)

func TestAIExportIncludesEveryViewAndDomainWithoutExportingConnectionCredentials(t *testing.T) {
	m := testDashboard(t)
	m.source.Config.Host.SSH, m.source.Config.Host.IdentityFile = "private-ssh-user@private-host", "hidden-identity-file"
	m.source.Config.Sites[0].Repository = "https://owner:hidden-repository-password@example.invalid/repo?token=hidden-query"
	m.filter, m.siteID = "alpha", "alpha"
	m, cmd := press(m, "E")
	m = finish(m, cmd)
	if m.page != "export-ai" || m.menuFocus || !strings.Contains(m.frame(), "Export for AI") {
		t.Fatal("export view did not open")
	}
	m.aiExport.Directory = t.TempDir()
	m, cmd = press(m, "enter")
	if m.aiExport.Busy || !m.aiExport.Confirm || cmd != nil || !strings.Contains(m.frame(), "Export the current snapshot?") {
		t.Fatal("export action did not request confirmation")
	}
	m, cmd = press(m, "enter")
	if !m.aiExport.Busy || cmd == nil {
		t.Fatal("export did not start")
	}
	if duplicate := m.startAIExport(); duplicate != nil {
		t.Fatal("duplicate export started while busy")
	}
	m = finish(m, cmd)
	if m.aiExport.Busy || m.aiExport.Error != "" || m.aiExport.SavedPath == "" {
		t.Fatal("export failed", m.aiExport)
	}
	data, err := os.ReadFile(m.aiExport.SavedPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, title := range []string{"Overview", "Sites", "Issues", "Host metrics", "Inventory", "Disk usage", "Timeline", "Incidents", "Health log", "Security", "Capacity", "Impact", "Readiness", "Recovery", "Preflight", "Changes", "Deployments", "External", "Costs / setup"} {
		if !strings.Contains(text, " | "+title+" | ") {
			t.Fatal("view omitted from export", title)
		}
	}
	for _, domain := range []string{"alpha.example.invalid", "bravo.example.invalid", "charlie.example.invalid"} {
		if !strings.Contains(text, domain) {
			t.Fatal("selected UI filter leaked into export scope", domain)
		}
	}
	for _, secret := range []string{"private-ssh-user", "hidden-identity-file", "hidden-repository-password", "hidden-query"} {
		if strings.Contains(text, secret) {
			t.Fatal("connection/config secret leaked", secret)
		}
	}
	if m.aiExport.Sections != 19 || m.aiExport.Bytes != len(data) || !strings.Contains(m.frame(), "Saved Markdown") || !strings.Contains(text, "fictional") {
		t.Fatal("saved result did not describe the file")
	}
}

func TestAIExportFolderEditingSupportsPasteCancelAndUnicode(t *testing.T) {
	m := testDashboard(t)
	m.page, m.menuFocus = "export-ai", false
	m.ensureAIExport()
	original := m.aiExport.Directory
	m, _ = press(m, "P")
	m.input = ""
	folder := filepath.Join(t.TempDir(), "review folder", "évidence")
	v, _ := m.Update(tea.PasteMsg{Content: folder})
	m = v.(dashboard)
	if m.input != folder {
		t.Fatal("folder paste was changed", m.input)
	}
	m, _ = press(m, "esc")
	if m.aiExport.Directory != original {
		t.Fatal("cancel changed the destination")
	}
	m, _ = press(m, "P")
	m.input = folder
	m, cmd := press(m, "enter")
	m = finish(m, cmd)
	if m.aiExport.Directory != folder || m.inputKind != "" {
		t.Fatal("folder was not saved", m.aiExport.Directory)
	}
}

func TestAIExportFailureRetainsPreviousResultAndQuitContextCancelsCapture(t *testing.T) {
	m := testDashboard(t)
	m.aiExport = &aiExportState{Busy: true, SavedPath: "previous.md"}
	v, _ := m.Update(aiExportMsg{err: errors.New("directory unavailable")})
	m = v.(dashboard)
	if m.aiExport.Busy || m.aiExport.SavedPath != "previous.md" || !strings.Contains(m.aiExport.Error, "directory unavailable") {
		t.Fatal("export failure lost previous successful result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.source.AIExport(ctx); err == nil {
		t.Fatal("canceled capture continued")
	}
}

func TestAIExportNavigationClickAndNarrowLayout(t *testing.T) {
	m := testDashboard(t)
	m, _ = press(m, "up")
	if m.page != "export-ai" || !strings.Contains(strings.Join(m.navigationRows(100), "\n"), "EXPORT") {
		t.Fatal("export is not at the bottom of navigation")
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.navigationRows(100), "\n")), "Export for AI") {
		t.Fatal("export navigation label was clipped")
	}
	m, _ = press(m, "enter")
	for _, size := range [][2]int{{60, 16}, {80, 24}, {140, 40}} {
		m.width, m.height = size[0], size[1]
		rows := strings.Split(m.frame(), "\n")
		if len(rows) != m.height {
			t.Fatal("export frame height changed")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > m.width {
				t.Fatal("export exceeded terminal width")
			}
		}
	}
	m.width, m.height = 140, 40
	m.aiExport.Directory = t.TempDir()
	m, _ = press(m, "esc")
	for row, line := range m.liveLines() {
		if strings.Contains(ansi.Strip(line), "[Enter] Review export") {
			v, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 25, Y: row + 3})
			m = v.(dashboard)
			if cmd != nil || m.aiExport.Busy || !m.aiExport.Confirm {
				t.Fatal("click exported without a separate confirmation")
			}
			for confirmRow, confirmLine := range m.liveLines() {
				if strings.Contains(ansi.Strip(confirmLine), "[Enter] Confirm export") {
					v, cmd = m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 25, Y: confirmRow + 3})
					m = v.(dashboard)
					if cmd == nil || !m.aiExport.Busy || m.aiExport.Confirm {
						t.Fatal("confirmation click did not start export")
					}
					m = finish(m, cmd)
					return
				}
			}
			t.Fatal("confirmation action was missing")
		}
	}
	t.Fatal("export action was missing")
}

func TestAIExportMenuRequiresConfirmationAndCancellationNeverWrites(t *testing.T) {
	m := testDashboard(t)
	m, cmd := press(m, "up") // Wrap from Overview to the bottom Export entry.
	m = finish(m, cmd)
	directory := t.TempDir()
	m.aiExport.Directory, m.aiExport.SavedPath = directory, "previous.md"
	assertNoFile := func() {
		t.Helper()
		files, err := os.ReadDir(directory)
		if err != nil || len(files) != 0 || m.aiExport.Busy || m.aiExport.SavedPath != "previous.md" {
			t.Fatal("navigation/cancellation wrote a file or lost the previous export", files, err)
		}
	}
	assertNoFile()
	if !m.menuFocus || m.aiExport.Confirm {
		t.Fatal("browsing the menu armed the export")
	}
	if cmd = m.startAIExport(); cmd != nil {
		t.Fatal("an unconfirmed export could start")
	}
	m, cmd = press(m, "enter")
	if cmd != nil || !m.aiExport.Confirm || m.menuFocus || !strings.Contains(strings.Join(m.aiExportLines(4096), "\n"), directory) {
		t.Fatal("menu Enter did not open the confirmation with its destination")
	}
	assertNoFile()
	for _, size := range [][2]int{{60, 16}, {80, 24}} {
		m.width, m.height = size[0], size[1]
		if !strings.Contains(m.frame(), "Confirm export") || !strings.Contains(m.frame(), "Cancel") {
			t.Fatal("confirmation controls were hidden on a small terminal")
		}
	}
	m, cmd = press(m, "esc")
	if cmd != nil || m.aiExport.Confirm || !strings.Contains(m.frame(), "Review export") {
		t.Fatal("Escape did not cancel the confirmation")
	}
	assertNoFile()
	m, _ = press(m, "enter")
	m, _ = press(m, "P")
	if m.aiExport.Confirm || m.inputKind != "export-directory" {
		t.Fatal("changing the destination kept an old confirmation armed")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "enter")
	m, cmd = press(m, "o")
	m = finish(m, cmd)
	if m.aiExport.Confirm {
		t.Fatal("leaving the view kept confirmation armed")
	}
	m, cmd = press(m, "E")
	m = finish(m, cmd)
	if m.aiExport.Confirm {
		t.Fatal("reopening the export page restored an old confirmation")
	}
	assertNoFile()
}

func TestAIHistorySummaryPreservesMeasuredZeroAndRejectsOutsideWindow(t *testing.T) {
	now := time.Now()
	history := summarizeAIHistory([]metrics.Sample{{ObservedAt: now.Add(-time.Hour), Values: map[string]float64{"cpu_percent": 90}}, {ObservedAt: now.Add(time.Hour), Values: map[string]float64{"cpu_percent": 99}}, {ObservedAt: now.Add(-time.Minute), Values: map[string]float64{"cpu_percent": 5}}, {ObservedAt: now, Values: map[string]float64{"cpu_percent": 0}}}, now)
	if history.Samples != 2 || len(history.Observations) != 2 || history.Observations[1].Values["cpu_percent"] != 0 || len(history.Statistics) != 1 || history.Statistics[0].Min != 0 || history.Statistics[0].Max != 5 || history.Statistics[0].Latest != 0 {
		t.Fatal("metric summary fabricated missing/old/future samples", history)
	}
}
