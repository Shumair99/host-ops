package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/storage"
)

func query(ctx context.Context, c config.Config, command, id, db, since string, out, errOut io.Writer) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
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
		encoder.Encode(metrics.Disk(ctx, platform.Command{Host: c.Host}, c.Host))
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
	kind := "health"
	if command == "events" {
		kind = "event"
	}
	if command == "deployments" {
		kind = "deployment"
	}
	records, err := store.Query(ctx, c.Host.ID, id, kind, time.Now().Add(-duration), 1000)
	if err != nil {
		fmt.Fprintln(errOut, "History query failed")
		return 1
	}
	encoder.Encode(records)
	return 0
}
