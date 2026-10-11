package tui

import (
	"fmt"
	"strings"
)

func (m dashboard) helpWorkspace(w int) []string {
	t := m.theme
	groups := []struct {
		title string
		rows  []string
	}{
		{"Navigate", []string{"Ctrl+P        Search every view and property", "↑↓ / j k      Move through lists; ends wrap around", "Mouse wheel   Scroll the pane under the pointer", "Enter / →     Focus content or open a property", "← / Tab       Move between menu and content", "Esc           Go back one level; cancel input", "PgUp / PgDn   Scroll a page; Home / End go to edges", "S             Billing settings or evidence setup"}},
		{"Investigate", []string{"s             Sites · / filters · 2 cycles sort", "i             Issues · shared evidence consolidated", "h             Host · CPU, memory, network, pressure", "d             Disk · Enter opens a directory", "Backspace     Parent directory; Space rescans Disk", "l / a / b     Property logs / readiness / recovery", "x             Impact · / chooses a dependency", "w             Incidents · export selected capsule", "↑↓            Select readiness / recovery property", "1             Show passing readiness / security rules"}},
		{"Observe", []string{"Space         Reload the current evidence", "F5            Request inventory and public checks", "E             Open Export for AI", "Enter         On Export: review, then confirm to save", "P             On Export: choose destination folder", "t             Demo outage / recovery", "q / Ctrl+C    Quit and restore the terminal", "Host readings refresh every second; inventory / workloads every 30s.", "Public HTTP uses --interval; DNS / TLS checks run every 5 minutes."}},
		{"Read the instruments", []string{"+ pass   ! fail   ~ warn   ? unknown   − inapplicable", "Host graphs use a fixed 30-minute window. Gaps remain blank.", "Percentiles use recorded samples; they are not time-weighted.", "Traffic covers non-loopback interfaces, including virtual traffic.", "Observation maps show samples; they do not prove continuous uptime.", "Disk ≥ totals are lower bounds; directory / shared totals can overlap.", "Billing is estimated; recovery records are operator-reported.", "Detailed limits and setup: docs/terminal-guide.md"}},
	}
	out := []string{}
	for i := 0; i < len(groups); i += 2 {
		if w >= 88 {
			a := (w - 1) / 2
			out = append(out, columns(t.panel(groups[i].title, groups[i].rows, a), t.panel(groups[i+1].title, groups[i+1].rows, w-a-1), a, w-a-1)...)
		} else {
			out = append(out, t.panel(groups[i].title, groups[i].rows, w)...)
			out = append(out, t.panel(groups[i+1].title, groups[i+1].rows, w)...)
		}
	}
	return out
}

func (m dashboard) logsWorkspace(w int) []string {
	t := m.theme
	rows := []string{t.paint(fmt.Sprintf("%d lines · common secret patterns redacted", len(m.lines)), muted)}
	for i, raw := range m.lines {
		line := consoleLine(raw)
		color := ink
		if strings.Contains(strings.ToLower(line), "error") || strings.Contains(strings.ToLower(line), "fatal") {
			color = red
		} else if strings.Contains(strings.ToLower(line), "warn") {
			color = amber
		}
		rows = append(rows, t.paint(fmt.Sprintf("%3d │ ", i+1), muted)+t.paint(line, color))
	}
	if len(m.lines) == 0 {
		rows = append(rows, "No log lines returned")
	}
	return t.panel("Runtime logs", rows, w)
}
