package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/storage"
)

const DiskRefreshInterval = 30 * time.Minute

type DiskScan struct {
	StartedAt    time.Time `json:"started_at"`
	RunningUntil time.Time `json:"running_until"`
	FinishedAt   time.Time `json:"finished_at"`
	Issue        string    `json:"issue,omitempty"`
}

func DiskDue(report *metrics.DiskReport, scan DiskScan, now time.Time) bool {
	if now.Before(scan.RunningUntil) {
		return false
	}
	if scan.Issue != "" && !scan.StartedAt.IsZero() && now.Sub(scan.StartedAt) < 5*time.Minute {
		return false
	}
	if scan.Issue != "" {
		return true
	}
	if report == nil {
		return true
	}
	if report.CompletedAt.IsZero() {
		return true // Upgrade earlier shallow inspections.
	}
	return now.Sub(report.CompletedAt) >= DiskRefreshInterval
}

func DiskState(ctx context.Context, store *storage.Store, host string) (*metrics.DiskReport, DiskScan, error) {
	var report *metrics.DiskReport
	var scan DiskScan
	data, err := store.State(ctx, host, "", "disk_inspection")
	if err != nil {
		return nil, scan, err
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &report); err != nil {
			return nil, scan, err
		}
	}
	data, err = store.State(ctx, host, "", "disk_scan")
	if err != nil {
		return report, scan, err
	}
	if len(data) > 0 {
		err = json.Unmarshal(data, &scan)
	}
	return report, scan, err
}

func InspectDisk(ctx context.Context, c config.Config, store *storage.Store, force bool) (*metrics.DiskReport, error) {
	previous, scan, err := DiskState(ctx, store, c.Host.ID)
	if err != nil {
		return previous, err
	}
	now := time.Now().UTC()
	if !force && !DiskDue(previous, scan, now) {
		return previous, nil
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return previous, err
	}
	owner := hex.EncodeToString(token)
	acquired, err := store.AcquireLease(ctx, c.Host.ID, "disk_inspection", owner, now, 75*time.Second)
	if err != nil || !acquired {
		return previous, err
	}
	defer func() {
		if ctx.Err() == nil {
			cleanup, done := context.WithTimeout(context.Background(), 2*time.Second)
			defer done()
			_ = store.ReleaseLease(cleanup, c.Host.ID, "disk_inspection", owner)
		}
	}()
	previous, scan, err = DiskState(ctx, store, c.Host.ID)
	if err != nil {
		return previous, err
	}
	if !force && !DiskDue(previous, scan, time.Now()) {
		return previous, nil
	}
	scan = DiskScan{StartedAt: now, RunningUntil: now.Add(75 * time.Second)}
	if err = store.Transition(ctx, c.Host.ID, "", "disk_scan", scan, "", now, nil); err != nil {
		return previous, err
	}
	registry, err := store.Inventory(ctx, c.Host.ID)
	if err != nil {
		return previous, err
	}
	c, err = ResolveConfig(ctx, c, store)
	if err != nil {
		return previous, err
	}
	report := metrics.DiskWithInventory(ctx, platform.Command{Host: c.Host}, c.Host, registry, c.Sites)
	if ctx.Err() != nil {
		return previous, ctx.Err()
	}
	scan.RunningUntil = time.Time{}
	scan.FinishedAt = time.Now().UTC()
	if len(report.Directories) == 0 {
		scan.Issue = "Directory inspection unavailable"
		if previous == nil {
			previous = &report
		}
	} else {
		metrics.DiskGrowth(&report, previous)
		previous = &report
	}
	if err = store.Transition(ctx, c.Host.ID, "", "disk_inspection", previous, "", scan.FinishedAt, nil); err != nil {
		return previous, err
	}
	err = store.Transition(ctx, c.Host.ID, "", "disk_scan", scan, "", scan.FinishedAt, nil)
	return previous, err
}
