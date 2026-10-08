package agent

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/api"
	"github.com/Shumair99/host-ops/internal/changes"
	"github.com/Shumair99/host-ops/internal/config"
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
	if err := store.Reports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	if err := events.Record(ctx, store, previous, reports); err != nil {
		return err
	}
	if err := changes.Record(ctx, store, reports); err != nil {
		return err
	}
	for _, r := range reports {
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

func Run(ctx context.Context, c config.Config, store *storage.Store, address, token, webDir string, interval time.Duration, allowPrivate bool) error {
	if !api.Loopback(address) && !allowPrivate {
		return fmt.Errorf("management API must bind to an explicit loopback IP; use SSH tunnelling")
	}
	if len(token) < 32 {
		return fmt.Errorf("HOSTOPS_TOKEN must contain at least 32 characters")
	}
	if interval < 5*time.Second {
		return fmt.Errorf("collection interval must be at least 5s")
	}
	server := &http.Server{Addr: address, Handler: (&api.Server{Config: c, Store: store, Token: token, MaxAge: interval * 3, WebDir: webDir}).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	collectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for {
			if err := Cycle(collectionCtx, c, store); err != nil && collectionCtx.Err() == nil {
				log.Printf("collection failed for host %s", c.Host.ID)
			}
			timer := time.NewTimer(interval)
			select {
			case <-collectionCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	select {
	case <-ctx.Done():
	case err := <-errors:
		cancel()
		<-collectorDone
		return err
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	err := server.Shutdown(shutdownCtx)
	<-collectorDone
	return err
}
