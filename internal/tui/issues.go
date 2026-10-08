package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/model"
)

func issueLines(snapshot Snapshot) []string {
	lines := []string{}
	if disk, ok := snapshot.Host.Values["disk_percent"]; ok && disk >= 80 {
		lines = append(lines, fmt.Sprintf("[WARN] Root filesystem / %.1f%% used", disk))
	}
	if len(snapshot.Sites) == 0 {
		lines = append(lines, "[UNKNOWN] No discovered sites")
	}
	type issue struct {
		check    model.Check
		affected []string
	}
	grouped := map[string]*issue{}
	reports := reportsByID(snapshot.Reports)
	for _, site := range snapshot.Sites {
		report := domainReport(site, reports)
		for _, check := range report.Checks {
			if check.State == model.Pass || check.State == model.Skip {
				continue
			}
			key := string(check.State) + "\x00" + check.Layer + "\x00" + check.Summary
			if grouped[key] == nil {
				grouped[key] = &issue{check: check}
			}
			label := report.Name
			if site.Proxy.Domain != "" {
				label = site.Proxy.Domain
			}
			if label == "" {
				label = report.SiteID
			}
			if !contains(grouped[key].affected, label) {
				grouped[key].affected = append(grouped[key].affected, label)
			}
		}
	}
	keys := []string{}
	for key := range grouped {
		keys = append(keys, key)
	}
	priority := map[model.State]int{model.Fail: 0, model.Warn: 1, model.Unknown: 2}
	sort.Slice(keys, func(i, j int) bool {
		a, b := grouped[keys[i]].check, grouped[keys[j]].check
		if priority[a.State] != priority[b.State] {
			return priority[a.State] < priority[b.State]
		}
		return keys[i] < keys[j]
	})
	for _, key := range keys {
		item := grouped[key]
		lines = append(lines, fmt.Sprintf("[%s] %s / %d domains", strings.ToUpper(string(item.check.State)), layerLabel(item.check.Layer), len(item.affected)), "  "+console.Safe(item.check.Summary), "  "+console.Safe(strings.Join(item.affected, ", ")), "")
	}
	if len(lines) == 0 {
		return []string{"No observed exceptions in the current evidence."}
	}
	return lines
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
