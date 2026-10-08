package tui

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/console"
)

func toolsLines(c config.Config, selected int) []string {
	descriptions := map[string]string{
		"security":    "Host exposure and runtime posture; inspection on demand.",
		"capacity":    "Measured percentiles and disk growth; history required.",
		"impact":      "Explore affected sites for a host or dependency.",
		"readiness":   "Production checklist; some rules need operator evidence.",
		"recovery":    "Backup and recorded restore evidence; imports required.",
		"preflight":   "Read-only maintenance assessment; no reboot is performed.",
		"changes":     "Infrastructure changes, also included in Timeline.",
		"deployments": "Deployment records, also included in Timeline; imports required.",
		"external":    "Independent watcher records; import from another machine.",
		"costs":       "Optional billing setup and estimated allocation.",
	}
	lines := []string{"OPTIONAL TOOLS", "Choose with Up/Down, then Enter. View letters still work.", ""}
	for i, item := range toolMenu {
		cursor := " "
		if i == selected {
			cursor = ">"
		}
		lines = append(lines, cursor+" ["+item.Key+"] "+item.Label, "    "+descriptions[item.Page])
	}
	if !c.Host.BillingEnabled() {
		if c.Host.Billing != nil && c.Host.Billing.Provider == "self-hosted" {
			lines = append(lines, "", "Cost tracking is disabled for own hardware. Monitoring still works.")
		} else {
			lines = append(lines, "", "Costs need billing settings; nothing is inferred from CPU usage.")
		}
	}
	return lines
}

func inspectionPreview(page string, snapshot Snapshot) []string {
	lines := []string{"No inspection recorded"}
	if page == "disk" {
		lines = append(lines, console.Bar(snapshot.Host.Values, "disk_percent", "DISK"))
	}
	return lines
}
