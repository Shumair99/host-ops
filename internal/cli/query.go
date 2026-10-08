package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/preflight"
	"github.com/Shumair99/host-ops/internal/readiness"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/security"
	"github.com/Shumair99/host-ops/internal/storage"
)

func query(ctx context.Context, c config.Config, command, id, db, since string, out, errOut io.Writer) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if command == "impact" {
		report, err := preflight.Dependency(c, id)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		encoder.Encode(report)
		return 0
	}
	if command == "ownership" {
		encoder.Encode(security.Ownership(c, id))
		return 0
	}
	if command == "logs" {
		if id == "" {
			fmt.Fprintln(errOut, "logs requires a site")
			return 1
		}
		sites, _ := c.Select(id)
		text, err := logs.Read(ctx, platform.Command{Host: c.Host}, sites[0])
		if err != nil {
			fmt.Fprintln(errOut, "Log stream unavailable")
			return 1
		}
		fmt.Fprintln(out, text)
		return 0
	}
	if command == "disk" {
		store, err := storage.Open(db)
		if err != nil {
			fmt.Fprintln(errOut, "Disk history unavailable")
			return 1
		}
		defer store.Close()
		report, err := agent.InspectDisk(ctx, c, store, true)
		if err != nil {
			fmt.Fprintln(errOut, "Disk inspection unavailable")
			return 1
		}
		encoder.Encode(report)
		return 0
	}
	if command == "metrics" {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		runner := platform.Command{Host: c.Host}
		if id == "" {
			encoder.Encode(metrics.Host(ctx, runner, c.Host))
		} else {
			sites, _ := c.Select(id)
			encoder.Encode(metrics.Container(ctx, runner, sites[0]))
		}
		return 0
	}
	duration, err := time.ParseDuration(since)
	if err != nil || duration <= 0 {
		fmt.Fprintln(errOut, "since must be a positive duration")
		return 1
	}
	if _, err = os.Stat(db); err != nil {
		fmt.Fprintln(errOut, "History database does not exist; run the agent first")
		return 1
	}
	store, err := storage.Open(db)
	if err != nil {
		fmt.Fprintln(errOut, "History database unavailable")
		return 1
	}
	defer store.Close()
	if err = store.Prune(ctx, c.History); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if command == "incidents" {
		value, err := incidents.List(ctx, store, c.Host.ID, id)
		if err != nil {
			fmt.Fprintln(errOut, "Incident history unavailable")
			return 1
		}
		encoder.Encode(value)
		return 0
	}
	if command == "events" {
		records, err := store.Activity(ctx, c.Host.ID, id, time.Now().Add(-duration), 1000)
		if err != nil {
			fmt.Fprintln(errOut, "Timeline query failed")
			return 1
		}
		encoder.Encode(records)
		return 0
	}
	if command == "capsule" {
		value, err := incidents.Export(ctx, store, c.Host.ID, id)
		if err != nil {
			fmt.Fprintln(errOut, "Incident not found")
			return 1
		}
		encoder.Encode(value)
		return 0
	}
	if command == "preflight" {
		reports, err := store.Latest(ctx, c.Host.ID)
		if err != nil {
			fmt.Fprintln(errOut, "Preflight evidence unavailable")
			return 1
		}
		report, err := preflight.Assess(ctx, c, id, reports, store, platform.Command{Host: c.Host})
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		encoder.Encode(report)
		if report.Overall != "healthy" {
			return 2
		}
		return 0
	}
	if command == "recovery" {
		sites, _ := c.Select(id)
		reports := []recovery.Assessment{}
		for _, site := range sites {
			report, err := recovery.Assess(ctx, c, site, store)
			if err != nil {
				fmt.Fprintln(errOut, "Recovery evidence unavailable")
				return 1
			}
			reports = append(reports, report)
		}
		encoder.Encode(reports)
		return 0
	}
	if command == "costs" {
		report, err := costs.Assess(ctx, c, store)
		if err != nil {
			fmt.Fprintln(errOut, "Cost evidence unavailable")
			return 1
		}
		encoder.Encode(report)
		return 0
	}
	if command == "capacity" {
		report, err := capacity.Assess(ctx, c, store)
		if err != nil {
			fmt.Fprintln(errOut, "Capacity evidence unavailable")
			return 1
		}
		encoder.Encode(report)
		return 0
	}
	if command == "security" || command == "orphans" {
		reports, err := store.Latest(ctx, c.Host.ID)
		if err != nil {
			fmt.Fprintln(errOut, "Runtime evidence unavailable")
			return 1
		}
		report := security.Inspect(ctx, c, reports, platform.Command{Host: c.Host})
		if command == "orphans" {
			encoder.Encode(report.PossibleOrphans)
		} else {
			encoder.Encode(report)
		}
		return 0
	}
	if command == "readiness" {
		reports, err := store.Latest(ctx, c.Host.ID)
		if err != nil {
			fmt.Fprintln(errOut, "Health history unavailable")
			return 1
		}
		byID := map[string]model.Report{}
		for _, r := range reports {
			byID[r.SiteID] = r
		}
		sites, _ := c.Select(id)
		assessments := []readiness.Assessment{}
		for _, s := range sites {
			a, err := readiness.Assess(ctx, c, s, byID[s.ID], store)
			if err != nil {
				fmt.Fprintln(errOut, "Readiness evidence unavailable")
				return 1
			}
			assessments = append(assessments, a)
		}
		encoder.Encode(assessments)
		return 0
	}
	kind := "health"
	if command == "deployments" {
		kind = "deployment"
	}
	if command == "diff" {
		kind = "change"
	}
	records, err := store.Query(ctx, c.Host.ID, id, kind, time.Now().Add(-duration), 1000)
	if err != nil {
		fmt.Fprintln(errOut, "History query failed")
		return 1
	}
	encoder.Encode(records)
	return 0
}
