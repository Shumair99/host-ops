package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func timelineLines(page string, records []storage.Record, identities ...map[string]string) []string {
	sort.SliceStable(records, func(i, j int) bool { return records[i].ObservedAt.After(records[j].ObservedAt) })
	if len(records) > 100 {
		records = records[:100]
	}
	title := strings.ToUpper(page)
	if page == "events" {
		title = "TIMELINE: HEALTH TRANSITIONS / CHANGES / DEPLOYMENTS"
	}
	if page == "monitoring" {
		title = "HEALTH HISTORY: INDEPENDENT DOMAIN OBSERVATIONS"
	}
	lines := []string{title, "24h / latest 100 records", ""}
	if len(records) == 0 {
		message := map[string]string{
			"events":      "No recorded activity",
			"changes":     "No infrastructure changes",
			"deployments": "No deployment records",
			"external":    "No watcher observations",
			"monitoring":  "No health observations",
		}[page]
		return append(lines, message)
	}
	for _, record := range records {
		label := record.SiteID
		if label == "" {
			label = "host"
		}
		if len(identities) > 0 && identities[0][record.SiteID] != "" {
			label = identities[0][record.SiteID]
		}
		if record.Kind == "health" || record.Kind == "external_health" {
			var report model.Report
			if json.Unmarshal(record.Data, &report) == nil {
				if report.Name != "" {
					label = report.Name
				}
				lines = append(lines, fmt.Sprintf("%s  %s [%s]", record.ObservedAt.Local().Format("02 Jan 15:04:05"), console.Safe(label), strings.ToUpper(report.Overall)))
				continue
			}
		}
		var data map[string]any
		json.Unmarshal(record.Data, &data)
		if name, _ := data["name"].(string); name != "" {
			label = name
		}
		summary := recordSummary(record.Data)
		if record.Kind == "event" || record.Kind == "incident" {
			summary = activitySummary(record)
		}
		if record.Kind == "change" {
			summary = "Changed: " + textField(data, "field")
		}
		if record.Kind == "deployment" {
			summary = "Deployment: " + textField(data, "status") + "  revision: " + textField(data, "commit") + "  health: " + textField(data, "health_result")
		}
		lines = append(lines, fmt.Sprintf("%s  %s / %s", record.ObservedAt.Local().Format("02 Jan 15:04:05"), console.Safe(label), record.Kind), "  "+console.Safe(summary))
		if before, ok := data["before"].(string); ok {
			lines = append(lines, "  "+console.Safe(before)+" -> "+textField(data, "after"))
		}
	}
	return lines
}

func textField(data map[string]any, key string) string {
	value, ok := data[key].(string)
	if !ok || value == "" {
		return "UNKNOWN"
	}
	return console.Safe(value)
}
