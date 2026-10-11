package tui

import (
	"fmt"
	"strings"
)

type destination struct{ Label, Hint, Page, SiteID string }

var viewHints = map[string]string{
	"overview": "Host pulse / health matrix", "sites": "Workloads / property inspector", "issues": "Exceptions / missing evidence",
	"host": "CPU / memory / network / pressure", "inventory": "Discovery / routes / coverage", "disk": "Filesystems / directories / storage",
	"events": "Health transitions / changes", "incidents": "Incident evidence / export", "monitoring": "Independent domain observations",
	"tools": "Assessments / evidence tools", "help": "Keyboard / monitoring guide", "security": "Permissions / published ports",
	"capacity": "Percentiles / disk forecast", "impact": "Dependency / affected properties", "readiness": "Production evidence score",
	"recovery": "Backup / restore evidence", "preflight": "Maintenance / reboot assessment", "changes": "Infrastructure change records",
	"deployments": "Pipeline / revisions / outcomes", "external": "Outside-in watcher records", "costs": "Budget / estimated allocation",
	"export-ai": "Markdown snapshot / AI review prompt",
}

func (m dashboard) destinations() []destination {
	out := []destination{}
	query := strings.ToLower(strings.TrimSpace(m.input))
	for _, v := range allViews() {
		if strings.Contains(strings.ToLower(v.Label+" "+viewHints[v.Page]), query) {
			out = append(out, destination{Label: v.Label, Hint: viewHints[v.Page], Page: v.Page})
		}
	}
	for _, g := range groupSites(m.rawSites()) {
		if strings.Contains(strings.ToLower(g.Primary.ID+" "+g.Primary.Name+" "+g.Primary.Proxy.Domain), query) {
			out = append(out, destination{Label: siteLabel(g.Primary), Hint: g.Primary.Proxy.Domain + " · property", Page: "site", SiteID: g.Primary.ID})
		}
		if query != "" {
			for _, s := range g.Children {
				if strings.Contains(strings.ToLower(s.ID+" "+s.Name+" "+s.Proxy.Domain), query) {
					out = append(out, destination{Label: clean(s.Proxy.Domain), Hint: siteLabel(g.Primary) + " · related domain", Page: "site", SiteID: s.ID})
				}
			}
		}
	}
	return out
}

func (m dashboard) commandLines(w int) []string {
	t, items := m.theme, m.destinations()
	limit := max(1, m.bodyHeight()-6)
	selected := min(m.commandSelected, max(0, len(items)-1))
	start := max(0, selected-limit+1)
	end := min(len(items), start+limit)
	rows := []string{t.paint(fmt.Sprintf("%d matches · views & properties", len(items)), muted), ""}
	for i := start; i < end; i++ {
		item := items[i]
		line := t.strong(pad(clean(item.Label), min(24, max(10, w/3))), ink) + "  " + t.paint(clean(item.Hint), muted)
		if i == selected {
			line = t.selection(line, w-4)
		}
		rows = append(rows, clip(line, w-4))
	}
	if len(items) == 0 {
		rows = append(rows, "No matches. Try a view name, domain or resource.")
	}
	rows = append(rows, "", t.paint("Type to search · ↑↓ choose · Enter open · Esc return", muted))
	return t.panel("Jump to", rows, w)
}

func (m dashboard) navigationRows(height int) []string {
	t := m.theme
	lines := []string{}
	selectedLine := 0
	groups := map[int]string{0: "MONITOR", 3: "EXPLORE", 6: "RECORDS", 9: "WORKSPACE", 11: "EXPORT"}
	for i, item := range menu {
		if label := groups[i]; label != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, t.paint(" "+label, muted))
		}
		line := " " + t.paint(item.Key, cyan) + " " + t.paint(item.Label, muted)
		if i == menuIndex(m.page) {
			line = t.strong(" "+item.Key+" "+item.Label, ink)
		}
		if i == m.navSelected {
			selectedLine = len(lines)
		}
		if m.menuFocus && i == m.navSelected {
			line = t.selection(" "+item.Key+" "+item.Label, 18)
		}
		lines = append(lines, line)
	}
	start := max(0, min(selectedLine-height/2, len(lines)-height))
	out := []string{}
	for i := start; i < min(len(lines), start+height); i++ {
		out = append(out, lines[i])
	}
	return out
}
