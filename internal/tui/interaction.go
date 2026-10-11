package tui

import (
	tea "charm.land/bubbletea/v2"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func wrapIndex(index, delta, count int) int {
	if count <= 0 {
		return 0
	}
	return ((index+delta)%count + count) % count
}

func (m *dashboard) moveSelection(delta int, loop bool) tea.Cmd {
	move := func(index, count int) int {
		if loop {
			return wrapIndex(index, delta, count)
		}
		return max(0, min(max(0, count-1), index+delta))
	}
	if m.menuFocus {
		return m.previewMenu(move(m.navSelected, len(menu)))
	}
	switch m.page {
	case "tools":
		m.toolSelected = move(m.toolSelected, len(toolMenu))
	case "sites":
		m.selected = move(m.selected, len(m.filteredSites()))
	case "disk":
		m.diskSelected = move(m.diskSelected, len(m.diskChildren()))
	default:
		if count := m.assessmentCount(); count > 0 {
			m.assessmentSelected = move(m.assessmentSelected, count)
			m.offset = 0
		} else {
			m.scroll(delta)
		}
	}
	return nil
}

func (m *dashboard) scroll(delta int) {
	maximum := max(0, len(wrap(m.liveLines(), m.bodyWidth()))-m.bodyHeight())
	m.offset = max(0, min(maximum, m.offset+delta))
}

func (m *dashboard) mouseWheel(msg tea.MouseWheelMsg) tea.Cmd {
	if msg.Y < 3 || msg.Y >= m.height-2 {
		return nil
	}
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return nil
	}
	if m.settings != nil {
		e := m.settings
		if e.Busy {
			return nil
		}
		count := 0
		if e.Step == "choose" {
			count = len(setupOptions(e.Kind))
			if e.Kind == "costs" {
				count = len(billingSources)
			}
		} else if e.Step == "resources" {
			count = len(e.Resources)
		}
		if count > 0 {
			e.Selected = max(0, min(count-1, e.Selected+delta))
		} else {
			m.scroll(3 * delta)
		}
		return nil
	}
	if m.inputKind == "command" {
		m.commandSelected = max(0, min(max(0, len(m.destinations())-1), m.commandSelected+delta))
		return nil
	}
	if m.inputKind != "" {
		return nil
	}
	m.menuFocus = msg.X < 19
	if m.menuFocus || m.page == "sites" || m.page == "tools" {
		return m.moveSelection(delta, false)
	}
	m.scroll(3 * delta)
	return nil
}

func (m *dashboard) mouseClick(msg tea.MouseClickMsg) tea.Cmd {
	if msg.Button != tea.MouseLeft || m.inputKind != "" || msg.Y < 3 || msg.Y >= m.height-2 {
		return nil
	}
	if m.settings != nil {
		return m.settingsClick(msg)
	}
	if msg.X < 19 {
		m.cancelAIExportConfirmation()
		rows := m.navigationRows(m.bodyHeight() - 1)
		row := msg.Y - 4 // body row zero is the VIEWS label
		if row < 0 || row >= len(rows) {
			return nil
		}
		label := strings.TrimSpace(ansi.Strip(rows[row]))
		for i, item := range menu {
			if strings.HasPrefix(label, item.Key+" ") {
				cmd := m.previewMenu(i)
				m.menuFocus = true
				return cmd
			}
		}
		return nil
	}
	m.menuFocus = false
	if m.page == "export-ai" {
		lines := wrap(m.liveLines(), m.bodyWidth())
		row := msg.Y - 3 + m.offset
		if row < len(lines) {
			if strings.Contains(lines[row], "[Enter] Review export") {
				m.requestAIExport()
				return nil
			}
			if strings.Contains(lines[row], "[Enter] Confirm export") {
				return m.startAIExport()
			}
			if strings.Contains(lines[row], "[Esc] Cancel") {
				m.cancelAIExportConfirmation()
				m.offset = 0
			}
			if strings.Contains(lines[row], "[P] Choose export folder") {
				m.editAIExportDirectory()
			}
		}
		return nil
	}
	if m.page == "costs" || m.page == "recovery" || m.page == "deployments" || m.page == "external" {
		lines := wrap(m.liveLines(), m.bodyWidth())
		row := msg.Y - 3 + m.offset
		if row < len(lines) && (strings.Contains(lines[row], "[Enter / S]") || strings.Contains(lines[row], "[S]")) {
			m.openSettings()
		}
	}
	return nil
}
