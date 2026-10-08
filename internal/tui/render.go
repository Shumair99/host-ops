package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/charmbracelet/x/ansi"
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

func allViews() []menuItem { return append(append([]menuItem(nil), menu...), toolMenu...) }
func (m dashboard) filteredSites() []config.Site {
	out := []config.Site{}
	for _, g := range groupSites(m.rawSites()) {
		if g.matches(m.filter) {
			out = append(out, g.Primary)
		}
	}
	return out
}
func (m dashboard) report(id string) model.Report {
	return groupReport(groupFor(m.rawSites(), id), reportsByID(m.snapshot.Reports))
}
func (m dashboard) bodyWidth() int  { return max(1, m.width-22) }
func (m dashboard) bodyHeight() int { return max(1, m.height-5) }
func (m dashboard) liveLines() []string {
	w := m.bodyWidth()
	switch m.page {
	case "overview":
		return m.overviewLines(w)
	case "sites":
		return m.siteTable(w, true)
	case "host":
		return m.hostLines(w)
	case "site":
		return m.inspectorLines(w)
	case "disk":
		return m.diskLines(w)
	case "inventory":
		return m.inventoryPanel(w)
	case "tools":
		return m.toolsPanel(w)
	case "issues":
		return m.theme.panel("Exceptions", m.decorate(issueLines(m.snapshot)), w)
	}
	if m.loading && len(m.lines) == 0 {
		return m.theme.panel(pageLabel(m.page), []string{m.theme.paint("Awaiting evidence…", muted)}, w)
	}
	if m.page == "help" {
		return m.theme.panel("Help / keyboard & monitoring", m.decorate(m.lines), w)
	}
	return m.detailLines(w)
}
func pageLabel(page string) string {
	for _, v := range allViews() {
		if v.Page == page {
			return v.Label
		}
	}
	return strings.Title(page)
}
func recordSummary(data []byte) string {
	var fields map[string]any
	if json.Unmarshal(data, &fields) == nil {
		for _, key := range []string{"summary", "finding", "type", "status", "overall"} {
			if v, ok := fields[key].(string); ok && v != "" {
				return clean(v)
			}
		}
	}
	return "Recorded event"
}
func (m dashboard) decorate(lines []string) []string {
	out := []string{}
	for _, raw := range lines {
		line := consoleLine(raw)
		color := ink
		s := strings.ToUpper(line)
		if strings.Contains(s, "[FAIL") || strings.Contains(s, "[CRITICAL") || strings.HasPrefix(s, "[OPEN]") {
			color = red
		} else if strings.Contains(s, "[WARN") {
			color = amber
		} else if strings.Contains(s, "[UNKNOWN") || strings.HasPrefix(s, "UNKNOWN") {
			color = muted
		} else if strings.Contains(s, "[PASS") || strings.Contains(s, "[HEALTHY") {
			color = mint
		}
		if strings.TrimSpace(line) != "" && line == strings.ToUpper(line) && color == ink {
			color = cyan
		}
		out = append(out, wrap([]string{m.theme.paint(line, color)}, max(1, m.bodyWidth()-4))...)
	}
	return out
}

// Untrusted record/log text is sanitized before adding renderer-owned styles.
func consoleLine(s string) string { return clean(s) }
func (m dashboard) frame() string {
	w, h := m.width, m.height
	t := m.theme
	if w < 60 || h < 16 {
		return "HOSTOPS\nResize to at least 60 columns × 16 rows.\nq quit"
	}
	mode := "LIVE / READ-ONLY"
	if m.source.Demo {
		mode = "DEMO / FICTIONAL DATA"
	} else if m.noCollect {
		mode = "RETAINED / AUTO OFF"
	}
	title := pageLabel(m.page)
	if m.siteID != "" {
		title = pageLabel(m.page) + " / " + clean(m.report(m.siteID).Name)
		if m.page == "site" {
			title = clean(m.report(m.siteID).Name)
		}
	}
	head := t.strong("HOSTOPS", cyan) + t.paint(" / ", edge) + t.strong(title, ink)
	right := t.paint(mode, muted)
	rows := []string{pad(head, max(1, w-ansi.StringWidth(right)-2)) + "  " + right}
	host := clean(m.source.Config.Host.Name)
	if host == "" {
		host = clean(m.source.Config.Host.ID)
	}
	state := "LOCAL HISTORY"
	if m.source.Demo {
		state = "OFFLINE"
	} else if m.source.Live {
		if m.snapshot.Connection.Connected {
			state = "CONNECTED"
		} else {
			state = "CONNECTING"
		}
	} else if m.collecting {
		state = "COLLECTING"
	}
	if m.collectErr != "" {
		state = "COLLECTION ERROR"
	}
	rows = append(rows, pad(t.paint(host, ink)+"  "+t.paint(state, statusColor(state))+"  "+t.paint("Observed "+ageLabel(m.snapshot.Host.ObservedAt), muted), w), t.paint(strings.Repeat("─", w), edge))
	content := wrap(m.liveLines(), m.bodyWidth())
	bodyHeight := m.bodyHeight()
	offset := min(m.offset, max(0, len(content)-bodyHeight))
	if m.page == "sites" {
		offset = min(max(0, m.selected-bodyHeight+5), max(0, len(content)-bodyHeight))
	} else if m.page == "tools" {
		offset = min(max(0, m.toolSelected-bodyHeight+4), max(0, len(content)-bodyHeight))
	}
	navHeight := bodyHeight - 1
	navStart := max(0, min(m.navSelected-navHeight/2, len(menu)-navHeight))
	for row := 0; row < bodyHeight; row++ {
		left := ""
		if row == 0 {
			left = t.strong("VIEWS", muted)
			if m.menuFocus {
				left = t.strong("VIEWS  ‹", cyan)
			}
		} else {
			i := navStart + row - 1
			if i < len(menu) {
				item := menu[i]
				left = " " + t.paint(item.Key, cyan) + "  " + t.paint(item.Label, muted)
				if i == menuIndex(m.page) {
					left = t.strong(" "+item.Key+"  "+item.Label, ink)
				}
				if m.menuFocus && i == m.navSelected {
					left = t.selection(" "+item.Key+"  "+item.Label, 18)
				}
			}
		}
		body := ""
		if row+offset < len(content) {
			body = content[row+offset]
		}
		rows = append(rows, pad(left, 18)+" "+t.paint("│", edge)+" "+pad(body, m.bodyWidth())+" ")
	}
	footer := m.keyBar()
	if m.notice != "" {
		footer = clean(m.notice)
	}
	if m.collectErr != "" {
		footer = "Collector unavailable · retained evidence"
	}
	if m.inputKind != "" {
		footer = clean(m.inputKind+": "+m.input) + "▏  Enter apply · Esc cancel"
	}
	if len(content) > bodyHeight {
		footer += fmt.Sprintf("  %d/%d", min(offset+bodyHeight, len(content)), len(content))
	}
	rows = append(rows, pad(t.paint(footer, muted), w), pad(t.paint("? help", cyan)+t.paint("   q quit", muted), w))
	return strings.Join(rows, "\n")
}
func (m dashboard) keyBar() string {
	if m.menuFocus {
		return "↑↓ view   → focus   letter jump"
	}
	switch m.page {
	case "sites":
		return "↑↓ site   Enter inspect   / filter   ← views"
	case "site":
		return "↑↓ scroll   l logs   a readiness   b recovery   Esc back"
	case "tools":
		return "↑↓ tool   Enter open   ← views"
	default:
		return "↑↓ scroll   Space refresh   ← views"
	}
}
