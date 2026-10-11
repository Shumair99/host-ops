package tui

import (
	"fmt"
	"sort"
	"strings"
)

type activeNotice struct {
	Label, State, Finding string
}

// Notices are derived from this snapshot, never from historical events. A
// domain's evidence is assessed independently before matching alias notices.
func (m dashboard) currentNotices() []activeNotice {
	notices := []activeNotice{}
	reports := reportsByID(m.snapshot.Reports)
	for _, g := range groupSites(m.rawSites()) {
		groupNotices := []activeNotice{}
		for _, s := range g.domains() {
			r := domainReport(s, reports)
			if r.Overall == "healthy" {
				continue
			}
			finding := primaryFinding(r)
			if finding == "" {
				finding = "Cause unconfirmed · " + attention(r)
			}
			duplicate := false
			for _, n := range groupNotices {
				duplicate = duplicate || n.State == r.Overall && n.Finding == finding
			}
			if duplicate {
				continue
			}
			label := s.Name
			if s.ID != g.Primary.ID || label == "" {
				label = s.Proxy.Domain
			}
			if label == "" {
				label = s.ID
			}
			groupNotices = append(groupNotices, activeNotice{Label: clean(label), State: r.Overall, Finding: finding})
		}
		notices = append(notices, groupNotices...)
	}
	for _, spec := range []struct{ key, label string }{{"cpu_percent", "CPU"}, {"memory_percent", "Memory"}, {"disk_percent", "Root disk"}} {
		if n := value(m.snapshot.Host.Values, spec.key); n != nil && *n >= 80 {
			state := "warning"
			if *n >= 95 {
				state = "critical"
			}
			notices = append(notices, activeNotice{Label: "Host", State: state, Finding: spec.label + " " + percent(n) + " used"})
		}
	}
	sort.SliceStable(notices, func(i, j int) bool {
		return stateRank(notices[i].State) < stateRank(notices[j].State)
	})
	return notices
}

func (m dashboard) operationsFeed(w, noticeLimit, activityLimit int) []string {
	t := m.theme
	rows := []string{t.strong("Pinned notices", ink)}
	notices := m.currentNotices()
	for _, n := range notices[:min(noticeLimit, len(notices))] {
		line := t.paint("["+strings.ToUpper(n.State)+"]", statusColor(n.State)) + " " + t.strong(n.Label, ink) + "  " + t.paint(n.Finding, amber)
		rows = append(rows, wrap([]string{line}, w-4)...)
	}
	if len(notices) == 0 {
		rows = append(rows, t.paint("No active notices", mint))
	} else if len(notices) > noticeLimit {
		rows = append(rows, t.paint(fmt.Sprintf("+ %d more active notices", len(notices)-noticeLimit), muted))
	}
	rows = append(rows, "", t.strong("Recent activity", ink))
	for _, r := range m.snapshot.Events[:min(activityLimit, len(m.snapshot.Events))] {
		rows = append(rows, clip(t.paint(r.ObservedAt.Local().Format("15:04"), muted)+"  "+t.paint(recordLabel(r, m.snapshot.Sites), cyan)+"  "+activitySummary(r), w-4))
	}
	if len(m.snapshot.Events) == 0 {
		rows = append(rows, t.paint("No recorded changes in 24h", muted))
	}
	return t.panel("Operations feed", rows, w)
}
