package tui

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

func (m dashboard) preflightRows(checks []model.Check, sites []config.Site, w int) []string {
	labels := timelineLabels(sites)
	order := []string{""}
	grouped := map[string][]model.Check{}
	for _, s := range config.OrderedSites(sites) {
		order = append(order, s.ID)
	}
	for _, check := range checks {
		grouped[check.SiteID] = append(grouped[check.SiteID], check)
	}
	rows := []string{}
	for _, id := range order {
		if len(grouped[id]) == 0 {
			continue
		}
		label := labels[id]
		if id == "" {
			label = "Host / shared infrastructure"
		}
		if label == "" {
			label = id
		}
		rows = append(rows, "", m.theme.strong(label, cyan))
		for _, c := range grouped[id] {
			rows = append(rows, m.theme.badge(string(c.State))+"  "+layerLabel(c.Layer))
			if c.Summary != "" {
				rows = append(rows, wrap([]string{m.theme.paint(clean(c.Summary), muted)}, w-4)...)
			}
		}
	}
	return rows
}
