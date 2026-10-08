package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/changes"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/events"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/storage"
)

func Cycle(ctx context.Context, c config.Config, store *storage.Store) error {
	reports := make([]model.Report, len(c.Sites))
	previous, err := store.Latest(ctx, c.Host.ID)
	if err != nil {
		return err
	}
	runner := &platform.Cache{Runner: platform.Command{Host: c.Host}}
	var workers sync.WaitGroup
	slots := make(chan struct{}, 3)
	for i, site := range c.Sites {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			workers.Wait()
			return ctx.Err()
		}
		workers.Add(1)
		go func(index int, site config.Site) {
			defer workers.Done()
			defer func() { <-slots }()
			reports[index] = health.Collect(ctx, c, site, runner)
		}(i, site)
	}
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	oldByID := map[string]model.Report{}
	for _, r := range previous {
		oldByID[r.SiteID] = r
	}
	for i := range reports {
		d, err := store.Query(ctx, c.Host.ID, reports[i].SiteID, "deployment", reports[i].ObservedAt.Add(-5*time.Minute), 10)
		if err != nil {
			return err
		}
		reports[i].Diagnoses = append(reports[i].Diagnoses, diagnostics.Correlate(oldByID[reports[i].SiteID], reports[i], d)...)
	}
	if err := store.Reports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	if err := events.Record(ctx, store, previous, reports); err != nil {
		return err
	}
	if err := changes.Record(ctx, store, reports); err != nil {
		return err
	}
	for _, r := range incidents.GroupReports(c, reports, "agent") {
		if _, err := incidents.Observe(ctx, store, r, "agent", 3, 2); err != nil {
			return err
		}
	}
	metricsCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// CPU needs two distinct counter samples; do not use the cycle cache here.
	host := metrics.Host(metricsCtx, runner.Runner, c.Host)
	if _, err := store.Add(ctx, c.Host.ID, "", "host_metrics", host.ObservedAt, host); err != nil {
		return err
	}
	for _, site := range c.Sites {
		m := metrics.Container(metricsCtx, runner, site)
		if _, err := store.Add(ctx, c.Host.ID, site.ID, "metrics", m.ObservedAt, m); err != nil {
			return err
		}
	}
	return store.Retain(ctx, 90*24*time.Hour)
}

func Run(ctx context.Context, c config.Config, store *storage.Store, interval time.Duration) error {
	if interval < 5*time.Second {
		return fmt.Errorf("collection interval must be at least 5s")
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := Cycle(ctx, c, store); err != nil && ctx.Err() == nil {
			log.Printf("collection failed for host %s", c.Host.ID)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
