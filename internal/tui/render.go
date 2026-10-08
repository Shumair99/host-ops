package tui

import (
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/model"
	"strings"
)

type menuItem struct{ Key, Page, Label string }

var menu = []menuItem{
	{"o", "overview", "Overview"}, {"s", "sites", "Sites"}, {"i", "issues", "Issues"},
	{"h", "host", "Host metrics"}, {"z", "inventory", "Inventory"}, {"d", "disk", "Disk usage"},
	{"e", "events", "Timeline"}, {"n", "incidents", "Incidents"}, {"v", "monitoring", "Health log"},
	{"a", "tools", "Tools"}, {"?", "help", "Help"},
}

var toolMenu = []menuItem{
	{"u", "security", "Security"}, {"p", "capacity", "Capacity"}, {"x", "impact", "Impact"},
	{"r", "readiness", "Readiness"}, {"b", "recovery", "Recovery"}, {"f", "preflight", "Preflight"},
	{"g", "changes", "Changes"}, {"y", "deployments", "Deployments"}, {"m", "external", "External"},
	{"c", "costs", "Costs / setup"},
}

func allViews() []menuItem {
	return append(append([]menuItem(nil), menu...), toolMenu...)
}

func (m dashboard) filteredSites() []config.Site {
	out := []config.Site{}
	for _, group := range groupSites(m.rawSites()) {
		if group.matches(m.filter) {
			out = append(out, group.Primary)
		}
	}
	return out
}
func (m dashboard) report(id string) model.Report {
	return groupReport(groupFor(m.rawSites(), id), reportsByID(m.snapshot.Reports))
}
func (m dashboard) liveLines() []string {
	lines := []string{}
	add := func(s string) { lines = append(lines, console.Safe(s)) }
	if m.page == "tools" {
		return toolsLines(m.source.Config, m.toolSelected)
	}
	if m.page == "sites" {
		nameWidth, compact := m.fleetLayout(true)
		add(fmt.Sprintf("  %-*s %-9s %s", nameWidth, "PROPERTY", "HEALTH", layerHeading(compact)))
		filter := m.filter
		if filter == "" {
			filter = "(all)"
		}
		add("Filter: " + filter)
		for i, s := range m.filteredSites() {
			cursor := " "
			if i == m.selected {
				cursor = ">"
			}
			r := m.report(s.ID)
			add(fmt.Sprintf("%s %-*s %-9s %s", cursor, nameWidth, clip(console.Safe(s.Name), nameWidth), strings.ToUpper(r.Overall), fleetChain(r, compact)))
		}
		add("")
		add("Up/Down select | Enter inspect | / filter")
		return lines
	}
	if m.page == "issues" {
		return issueLines(m.snapshot)
	}
	if m.page == "overview" {
		add("HOST " + m.source.Config.Host.ID + "  |  " + m.source.Config.Host.Name)
		if m.source.Live && !m.snapshot.Connection.Connected {
			add("Collector: connecting or unavailable")
			if m.snapshot.Connection.Issue != "" {
				add(m.snapshot.Connection.Issue)
			}
		}
		add(console.Bar(m.snapshot.Host.Values, "cpu_percent", "CPU"))
		add(console.Bar(m.snapshot.Host.Values, "memory_percent", "RAM"))
		add(console.Bar(m.snapshot.Host.Values, "disk_percent", "DISK"))
		if n, ok := m.snapshot.Host.Values["disk_free_bytes"]; ok {
			add("Available disk: " + console.Bytes(n))
		}
		add("Host observation: " + console.At(m.snapshot.Host.ObservedAt))
		add("")
		add("PRODUCTION PROPERTIES")
		nameWidth, compact := m.fleetLayout(false)
		add(fmt.Sprintf("%-*s %-9s %s", nameWidth, "PROPERTY", "HEALTH", layerHeading(compact)))
		for _, s := range m.sites() {
			r := m.report(s.ID)
			add(fmt.Sprintf("%-*s %-9s %s", nameWidth, clip(console.Safe(s.Name), nameWidth), strings.ToUpper(r.Overall), fleetChain(r, compact)))
		}
		add("")
		add("[+] pass  [!] fail  [~] warn  [?] unknown  [-] N/A")
		if n, ok := m.snapshot.Host.Values["disk_percent"]; ok && n >= 80 {
			add("")
			add(fmt.Sprintf("ATTENTION: root disk %.1f%% used. Press d to inspect.", n))
		}
		add("")
		add("RECENT EVENTS")
		if len(m.snapshot.Events) == 0 {
			add("No changes recorded yet.")
		}
		for _, r := range m.snapshot.Events {
			add(fmt.Sprintf("%s  %s  %s", console.At(r.ObservedAt), r.SiteID, recordSummary(r.Data)))
		}
		return lines
	}
	if m.loading {
		return []string{"Reading " + m.page + " evidence...", "", "You can switch views or quit while the read runs."}
	}
	return m.lines
}
func recordSummary(data []byte) string {
	var fields map[string]any
	if json.Unmarshal(data, &fields) == nil {
		for _, key := range []string{"summary", "finding", "type", "status", "overall"} {
			if value, ok := fields[key].(string); ok && value != "" {
				return console.Safe(value)
			}
		}
	}
	return "Recorded event"
}
func (m dashboard) fleetLayout(cursor bool) (int, bool) {
	width := m.width - 25
	compact := width < 50
	chainWidth, maximum := 23, 21
	if compact {
		chainWidth = 6
	}
	if cursor {
		width -= 2
		maximum = 19
	}
	return max(8, min(maximum, width-chainWidth-11)), compact
}
func layerHeading(compact bool) string {
	if compact {
		return "RLPDTH"
	}
	return " R   L   P   D   T   H"
}
func fleetChain(r model.Report, compact bool) string {
	s := chain(r)
	if compact {
		s = strings.NewReplacer("[", "", "]", "", " ", "").Replace(s)
	}
	return s
}
func clip(s string, width int) string {
	if width < 1 {
		return ""
	}
	if len(s) > width {
		if width > 3 {
			return s[:width-3] + "..."
		}
		return s[:width]
	}
	return s
}
func pad(s string, width int) string {
	s = strings.NewReplacer("\n", " ", "\t", " ").Replace(console.Safe(s))
	s = clip(s, width)
	return s + strings.Repeat(" ", max(0, width-len(s)))
}
func wrap(lines []string, width int) []string {
	out := []string{}
	for _, raw := range lines {
		for _, s := range strings.Split(console.Safe(raw), "\n") {
			s = strings.ReplaceAll(s, "\t", "  ")
			if s == "" {
				out = append(out, "")
				continue
			}
			for len(s) > width {
				cut := width
				if at := strings.LastIndex(s[:width], " "); at > width/2 {
					cut = at
				}
				out = append(out, s[:cut])
				s = strings.TrimLeft(s[cut:], " ")
			}
			out = append(out, s)
		}
	}
	return out
}
func (m dashboard) frame() string {
	width, height := m.width, m.height
	if width < 60 || height < 16 {
		return "AMADEUS HOSTOPS\nResize the terminal to at least 60 columns x 16 rows.\nPress q to quit."
	}
	navWidth := 18
	bodyWidth := width - navWidth - 7
	bodyHeight := height - 9
	border := "+" + strings.Repeat("-", width-2) + "+"
	line := func(s string) string { return "| " + pad(s, width-4) + " |" }
	mode := "LIVE / READ-ONLY"
	if m.source.Demo {
		mode = "DEMO / FICTIONAL DATA - NO NETWORK"
	} else if m.noCollect {
		mode = "AUTO COLLECTION OFF / MANUAL READS"
	}
	state := "IDLE"
	if m.source.Live {
		state = "STREAMING"
	}
	if m.collecting {
		state = "COLLECTING"
	}
	if m.collectErr != "" {
		state = "COLLECTION ERROR"
	}
	rows := []string{border, line("AMADEUS HOSTOPS  /  TERMINAL OPERATIONS DESK"), line(mode + "  |  " + m.source.Config.Host.ID + "  |  " + state), border}
	title := strings.ToUpper(m.page)
	if m.siteID != "" {
		title += " / " + m.siteID
	}
	menuTitle := "VIEWS"
	if m.menuFocus {
		menuTitle += " [FOCUS]"
	} else {
		title += " [FOCUS]"
	}
	rows = append(rows, "| "+pad(menuTitle, navWidth)+" | "+pad(title, bodyWidth)+" |")
	content := wrap(m.liveLines(), bodyWidth)
	offset := min(m.offset, max(0, len(content)-bodyHeight))
	if m.page == "sites" {
		offset = min(max(0, m.selected-bodyHeight+4), max(0, len(content)-bodyHeight))
	} else if m.page == "tools" {
		offset = min(max(0, 2*m.toolSelected-bodyHeight+5), max(0, len(content)-bodyHeight))
	}
	active := m.navSelected
	navStart := max(0, min(active-bodyHeight/2, len(menu)-bodyHeight))
	for row := 0; row < bodyHeight; row++ {
		left := ""
		index := navStart + row
		if index < len(menu) {
			item := menu[index]
			cursor := " "
			if m.menuFocus && index == m.navSelected {
				cursor = ">"
			} else if index == menuIndex(m.page) {
				cursor = "*"
			}
			left = cursor + "[" + item.Key + "] " + item.Label
		}
		right := ""
		if row+offset < len(content) {
			right = content[row+offset]
		}
		rows = append(rows, "| "+pad(left, navWidth)+" | "+pad(right, bodyWidth)+" |")
	}
	rows = append(rows, border)
	notice := m.notice
	if m.inputKind != "" {
		notice = m.inputKind + ": " + m.input + "_  (Enter apply, Esc cancel)"
	} else if m.collectErr != "" {
		notice = "Collection failed; check SSH/manifest. Previous evidence remains."
	}
	if notice == "" {
		if m.menuFocus {
			notice = "Menu: Up/Down preview | Right/Enter focus content | letter shortcut"
		} else {
			notice = fmt.Sprintf("%d-%d of %d lines | Up/Down scroll | Left menu | Space refresh", min(offset+1, len(content)), min(offset+bodyHeight, len(content)), len(content))
		}
	}
	rows = append(rows, line(notice), line("Tab switch pane | F5 collect | Enter inspect site | ? help | q quit"), border)
	return strings.Join(rows, "\n")
}
