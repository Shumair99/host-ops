package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
	{"E", "export-ai", "Export for AI"},
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
	if m.siteSort > 0 {
		reports := m.groupedReports()
		resources := map[string]*float64{}
		if m.siteSort == 2 || m.siteSort == 3 {
			key := "cpu_percent"
			if m.siteSort == 3 {
				key = "memory_used_bytes"
			}
			for _, s := range out {
				resources[s.ID] = m.propertyMetric(s.ID, key)
			}
		}
		sort.SliceStable(out, func(i, j int) bool {
			if m.siteSort == 1 {
				return stateRank(reports[out[i].ID].Overall) < stateRank(reports[out[j].ID].Overall)
			}
			if m.siteSort == 4 {
				return latencyNumber(reports[out[i].ID]) > latencyNumber(reports[out[j].ID])
			}
			a, b := resources[out[i].ID], resources[out[j].ID]
			if a == nil {
				return false
			}
			if b == nil {
				return true
			}
			return *a > *b
		})
	}
	return out
}
func (m dashboard) report(id string) model.Report {
	group := groupFor(m.rawSites(), id)
	reports := reportsByID(m.snapshot.Reports)
	if id != group.Primary.ID {
		for _, s := range group.Children {
			if s.ID == id {
				return domainReport(s, reports)
			}
		}
	}
	return groupReport(group, reports)
}
func (m dashboard) groupedReports() map[string]model.Report {
	byID := reportsByID(m.snapshot.Reports)
	out := map[string]model.Report{}
	for _, g := range groupSites(m.rawSites()) {
		out[g.Primary.ID] = groupReport(g, byID)
	}
	return out
}
func (m dashboard) bodyWidth() int  { return max(1, m.width-22) }
func (m dashboard) bodyHeight() int { return max(1, m.height-5) }
func (m dashboard) liveLines() []string {
	w := m.bodyWidth()
	if m.settings != nil {
		return m.settingsLines(w)
	}
	if m.inputKind == "command" {
		return m.commandLines(w)
	}
	switch m.page {
	case "overview":
		return m.overviewLines(w)
	case "sites":
		return m.sitesWorkspace(w)
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
		return m.issuesWorkspace(w)
	case "help":
		return m.helpWorkspace(w)
	case "export-ai":
		return m.aiExportLines(w)
	}
	if m.loading && len(m.lines) == 0 {
		return m.theme.panel(pageLabel(m.page), []string{m.theme.paint("Awaiting evidence…", muted)}, w)
	}
	if m.page == "logs" {
		return m.logsWorkspace(w)
	}
	lines := m.detailLines(w)
	switch m.page {
	case "costs":
		action := "[Enter / S] Set up billing"
		if m.source.Config.Host.Billing != nil {
			action = "[Enter / S] Edit billing settings"
		}
		return append(m.theme.panel("Billing", []string{m.theme.strong(action, cyan)}, w), lines...)
	case "recovery":
		return append(m.theme.panel("Recovery setup", []string{m.theme.strong("[S] Connect backup / restore evidence", cyan)}, w), lines...)
	case "deployments":
		return append(m.theme.panel("Connect deployments", []string{m.theme.strong("[Enter / S] Import a deployment report", cyan)}, w), lines...)
	case "external":
		return append(m.theme.panel("Connect a watcher", []string{m.theme.strong("[Enter / S] Import independent observations", cyan)}, w), lines...)
	}
	return lines
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
	} else if m.settings != nil {
		mode = "LOCAL SETTINGS"
	} else if m.noCollect {
		mode = "RETAINED / AUTO OFF"
	}
	title := pageLabel(m.page)
	if toolIndex(m.page) >= 0 && m.siteID == "" {
		title = "Tools / " + title
	}
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
	stateColor := statusColor(state)
	if m.propagating() {
		// Reserve all three dot cells so adjacent notices never move.
		state = pad("PROPAGATING"+strings.Repeat(".", m.propagationFrame+1), 14)
		stateColor = amber
	}
	if m.collectErr != "" {
		state = "COLLECTION ERROR"
		stateColor = red
	}
	header := t.paint(host, ink) + "  " + t.paint(state, stateColor)
	if m.snapshot.HistoryNotice != "" {
		header += t.paint("  HISTORY PRUNED · doctor for details", amber)
	}
	if m.source.Live && m.snapshot.Connection.Connected && !m.snapshot.Host.ObservedAt.IsZero() && time.Since(m.snapshot.Host.ObservedAt) > 5*time.Second {
		header += t.paint("  TELEMETRY STALE · "+ageLabel(m.snapshot.Host.ObservedAt), amber)
	}
	rows = append(rows, pad(header, w), t.paint(strings.Repeat("─", w), edge))
	content := wrap(m.liveLines(), m.bodyWidth())
	bodyHeight := m.bodyHeight()
	offset := min(m.offset, max(0, len(content)-bodyHeight))
	if m.page == "sites" {
		offset = 0 // The workspace windows its list and keeps selected details visible.
	} else if m.page == "tools" {
		offset = min(max(0, m.toolSelected-bodyHeight+4), max(0, len(content)-bodyHeight))
	}
	if m.inputKind == "command" {
		offset = 0
	}
	navigation := m.navigationRows(bodyHeight - 1)
	for row := 0; row < bodyHeight; row++ {
		left := ""
		if row == 0 {
			left = t.strong("VIEWS", muted)
			if m.menuFocus {
				left = t.strong("VIEWS  ‹", cyan)
			}
		} else if row-1 < len(navigation) {
			left = navigation[row-1]
		}
		body := ""
		if row+offset < len(content) {
			body = content[row+offset]
		}
		rows = append(rows, pad(left, 18)+" "+t.paint("│", edge)+" "+pad(body, m.bodyWidth())+" ")
	}
	footer := m.keyBar()
	if m.inputKind != "" {
		footer = clean(m.inputKind+": "+m.input) + "|  Enter apply · Esc cancel"
		if m.inputKind == "export-directory" {
			footer = "Folder: " + clean(m.input) + "|  Enter save · Esc cancel"
		}
	}
	if len(content) > bodyHeight {
		footer += fmt.Sprintf("  %d/%d", min(offset+bodyHeight, len(content)), len(content))
	}
	globalKeys := t.paint("Ctrl+P jump", cyan) + t.paint("   ? help   q quit", muted)
	if m.settings != nil || m.inputKind != "" {
		globalKeys = t.paint("Ctrl+C quit", muted)
	} else if m.collectErr != "" {
		globalKeys = t.paint("Collector unavailable · retained evidence", amber)
	} else if m.notice != "" {
		globalKeys = t.paint(clean(m.notice), muted)
	}
	rows = append(rows, pad(t.paint(footer, muted), w), pad(globalKeys, w))
	return strings.Join(rows, "\n")
}
func (m dashboard) keyBar() string {
	if m.settings != nil {
		if m.settings.Busy {
			return "Working…"
		}
		if m.settings.Step == "fields" {
			if m.settings.Selected < len(m.settings.Fields) && len(m.settings.Fields[m.settings.Selected].Choices) > 0 {
				return "Esc back   ←→ currency / click   Tab next   Enter next"
			}
			return "Esc back   Tab / ↑↓ field   Enter next / save   Ctrl+S save"
		}
		return "Esc back   ↑↓ choose   Enter continue"
	}
	if m.menuFocus {
		if m.page == "export-ai" {
			return "↑↓ view   Enter review export   → focus"
		}
		return "↑↓ view   → focus   letter jump"
	}
	switch m.page {
	case "sites":
		return "Esc back   ↑↓ site   Enter inspect   / filter   2 sort"
	case "disk":
		return "Esc back   ↑↓ dir   Enter open   Backspace parent   Space rescan"
	case "site":
		return "Esc back   ↑↓ scroll   l logs   a readiness   b recovery"
	case "tools":
		return "Esc back   ↑↓ tool   Enter open"
	case "readiness":
		return "Esc back   ↑↓ property   PgDn scroll   1 passed rules"
	case "recovery":
		return "Esc back   ↑↓ property   S setup   Wheel / PgDn scroll"
	case "incidents":
		return "Esc back   ↑↓ incident   PgDn scroll   w export selected"
	case "security":
		return "Esc back   ↑↓ / Wheel scroll   1 passed findings   Space refresh"
	case "export-ai":
		if m.aiExport != nil && m.aiExport.Confirm {
			return "Esc cancel   Enter confirm export   P folder"
		}
		if m.aiExport != nil && m.aiExport.Busy {
			return "Esc back   Exporting…   Wheel / PgDn scroll"
		}
		return "Esc back   Enter review export   P folder   Wheel / PgDn scroll"
	case "costs", "deployments", "external":
		return "Esc back   Enter / S setup   Wheel / PgDn scroll   Space refresh"
	default:
		if toolIndex(m.page) >= 0 && m.siteID == "" {
			return "Esc back   ↑↓ / Wheel scroll   Space refresh"
		}
		return "Esc back   ↑↓ / Wheel scroll   Space refresh"
	}
}
