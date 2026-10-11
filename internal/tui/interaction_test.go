package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestViewsAndEverySelectableListWrapBothDirections(t *testing.T) {
	m := testDashboard(t)
	m, _ = press(m, "up")
	if m.page != menu[len(menu)-1].Page || m.navSelected != len(menu)-1 {
		t.Fatal("Overview did not wrap to the last view")
	}
	m, _ = press(m, "down")
	if m.page != "overview" || m.navSelected != 0 {
		t.Fatal("Last view did not wrap to Overview")
	}
	for _, page := range []string{"sites", "tools", "disk", "readiness", "recovery", "incidents"} {
		t.Run(page, func(t *testing.T) {
			m := testDashboard(t)
			m.page, m.menuFocus = page, false
			count := 0
			switch page {
			case "sites":
				count = len(m.filteredSites())
			case "tools":
				count = len(toolMenu)
			case "disk":
				m.snapshot.Disk = &metrics.DiskReport{Roots: []string{"/"}, Directories: []metrics.DiskEntry{{Path: "/a", Parent: "/"}, {Path: "/b", Parent: "/"}}}
				count = 2
			case "readiness":
				m.snapshot.Detail = []readiness.Assessment{{SiteID: "a"}, {SiteID: "b"}}
				count = 2
			case "recovery":
				m.snapshot.Detail = []recovery.Assessment{{SiteID: "a"}, {SiteID: "b"}}
				count = 2
			case "incidents":
				m.snapshot.Detail = []storage.Record{{}, {}}
				count = 2
			}
			selected := func(m dashboard) int {
				switch page {
				case "sites":
					return m.selected
				case "tools":
					return m.toolSelected
				case "disk":
					return m.diskSelected
				default:
					return m.assessmentSelected
				}
			}
			m, _ = press(m, "up")
			if selected(m) != count-1 {
				t.Fatal("first entry did not wrap to last", selected(m), count)
			}
			m, _ = press(m, "down")
			if selected(m) != 0 {
				t.Fatal("last entry did not wrap to first")
			}
		})
	}
}

func TestJumpPaletteWrapsAndEmptyListsRemainUsable(t *testing.T) {
	m := testDashboard(t)
	m.inputKind = "command"
	m, _ = press(m, "up")
	if m.commandSelected != len(m.destinations())-1 {
		t.Fatal("jump list does not wrap")
	}
	m, _ = press(m, "down")
	if m.commandSelected != 0 {
		t.Fatal("jump list did not return to first")
	}
	m.input = "no-such-property-or-view"
	m, _ = press(m, "up")
	m, _ = press(m, "enter")
	if m.commandSelected != 0 || m.inputKind != "command" {
		t.Fatal("empty jump list navigated away")
	}
	m.inputKind, m.page, m.menuFocus, m.filter = "", "sites", false, "no-such-property"
	m, _ = press(m, "up")
	m, _ = press(m, "down")
	if m.selected != 0 {
		t.Fatal("empty site list produced an invalid selection")
	}
}

func TestMouseWheelScrollsContentUnderPointerAndStopsAtEdges(t *testing.T) {
	m := testDashboard(t)
	m.page, m.width, m.height, m.menuFocus = "host", 80, 20, true
	model, _ := m.Update(tea.MouseWheelMsg{X: 30, Y: 8, Button: tea.MouseWheelDown})
	m = model.(dashboard)
	if m.menuFocus || m.offset != 3 || m.page != "host" {
		t.Fatal("wheel over host content did not scroll its pane", m.offset)
	}
	maximum := len(wrap(m.liveLines(), m.bodyWidth())) - m.bodyHeight()
	m.offset = maximum
	model, _ = m.Update(tea.MouseWheelMsg{X: 30, Y: 8, Button: tea.MouseWheelDown})
	m = model.(dashboard)
	if m.offset != maximum {
		t.Fatal("wheel scrolled beyond content")
	}
	m.offset = 0
	model, _ = m.Update(tea.MouseWheelMsg{X: 30, Y: 8, Button: tea.MouseWheelUp})
	m = model.(dashboard)
	if m.offset != 0 || m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("wheel bounds or terminal mouse reporting incorrect")
	}
}

func TestEscapeReturnsThroughSiteAndToolsHierarchy(t *testing.T) {
	m := testDashboard(t)
	m.page, m.siteID, m.menuFocus = "logs", "alpha", false
	m, _ = press(m, "esc")
	if m.page != "site" || m.siteID != "alpha" {
		t.Fatal("site logs did not return to the same inspector")
	}
	m, _ = press(m, "esc")
	if m.page != "sites" {
		t.Fatal("site inspector did not return to Sites")
	}
	m.page, m.siteID, m.menuFocus = "security", "", false
	m, _ = press(m, "esc")
	if m.page != "tools" || m.toolSelected != toolIndex("security") {
		t.Fatal("tool did not return to its Tools entry")
	}
	m, _ = press(m, "esc")
	if !m.menuFocus {
		t.Fatal("Tools did not return focus to Views")
	}
}

func TestStatusMessagesDoNotHideBackControls(t *testing.T) {
	m := testDashboard(t)
	m.page, m.menuFocus, m.notice, m.collectErr = "security", false, "Settings saved", "collector failed"
	if !strings.Contains(m.frame(), "Esc back") || !strings.Contains(m.frame(), "Collector unavailable") {
		t.Fatal("collector status replaced navigation controls")
	}
	m.page = "costs"
	m.openSettings()
	if !strings.Contains(m.frame(), "Esc back") || strings.Contains(m.frame(), "Collector unavailable") {
		t.Fatal("unrelated collector error hides local settings controls")
	}
}
