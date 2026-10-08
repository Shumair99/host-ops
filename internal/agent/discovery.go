package agent

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/collector"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/events"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"sync"
	"time"
)

type Connection struct {
	Connected  bool      `json:"connected"`
	ObservedAt time.Time `json:"observed_at"`
	Issue      string    `json:"issue,omitempty"`
}
type publicResult struct {
	checks    map[string][]model.Check
	networkAt map[string]time.Time
}

func ResolveConfig(ctx context.Context, c config.Config, store *storage.Store) (config.Config, error) {
	if !c.Discovering() {
		return c, nil
	}
	registry, err := store.Inventory(ctx, c.Host.ID)
	if err != nil {
		return c, err
	}
	return discovery.ConfigSites(registry, c), nil
}

func connection(ctx context.Context, store *storage.Store, host string, connected bool, issue string) error {
	return store.Transition(ctx, host, "", "connection", Connection{connected, time.Now().UTC(), issue}, "", time.Now(), nil)
}

func baseReports(c config.Config, registry discovery.Registry, public map[string][]model.Check) []model.Report {
	items := map[string]discovery.Site{}
	for _, item := range registry.Sites {
		items[item.Domain] = item
	}
	workloads := map[string]discovery.Workload{}
	for _, w := range registry.Snapshot.Workloads {
		workloads[w.ID] = w
	}
	sources := map[string]discovery.Source{}
	for _, source := range registry.Snapshot.Sources {
		sources[source.ID] = source
	}
	reports := []model.Report{}
	for _, site := range c.Sites {
		item, ok := items[site.Proxy.Domain]
		if !ok {
			continue
		}
		r := model.Report{HostID: c.Host.ID, SiteID: site.ID, Name: site.Name, ObservedAt: time.Now().UTC(), Vantage: "SSH inventory + client public probes", Runtime: model.Runtime{HostID: c.Host.ID, SiteID: site.ID, Type: site.Runtime.Type, Name: site.Runtime.Container}, Checks: []model.Check{}, Diagnoses: []model.Diagnosis{}}
		check := func(layer string, state model.State, summary string) {
			r.Checks = append(r.Checks, model.Check{HostID: c.Host.ID, SiteID: site.ID, Layer: layer, State: state, Summary: summary})
		}
		if len(item.WorkloadIDs) == 0 {
			state := model.Unknown
			text := "Backend ownership unresolved; configured site remains visible"
			for _, route := range item.Routes {
				if route.Kind == "static" || route.Kind == "redirect" || route.Kind == "response" {
					state = model.Skip
					text = "Route is served by the proxy"
				}
			}
			check("runtime", state, text)
		}
		for index, id := range item.WorkloadIDs {
			w := workloads[id]
			state := model.Pass
			if w.Status != "running" && w.Status != "active" {
				state = model.Fail
			}
			if sources[w.SourceID].State != "ready" {
				state = model.Unknown
			}
			check("runtime", state, w.Name+": "+w.Status)
			if index == 0 {
				r.Runtime.Name = w.Name
				r.Runtime.Type = w.Type
				r.Runtime.Status = w.Status
				r.Runtime.Health = w.Health
				r.Runtime.Image = w.Image
				r.Runtime.Commit = w.Revision
				r.Runtime.RestartPolicy = w.RestartPolicy
				r.Runtime.IPs = map[string]string{}
				for n, net := range w.Networks {
					r.Runtime.IPs[n] = net.IP
				}
			}
			if w.Health != "" && w.Health != "none" {
				state = model.Unknown
				switch w.Health {
				case "healthy":
					state = model.Pass
				case "unhealthy":
					state = model.Fail
				case "starting":
					state = model.Warn
				}
				check("container_health", state, w.Name+": "+w.Health)
			}
		}
		proxyState, summary := model.Warn, "Declared route found; loaded state unverified"
		for _, route := range item.Routes {
			source := sources[route.SourceID]
			if source.State == "ready" && source.Mode == "active" {
				proxyState, summary = model.Pass, "Domain present in active proxy configuration"
				break
			}
		}
		if item.State == "stale" {
			proxyState, summary = model.Unknown, "Route inventory is stale or disappeared pending confirmation"
		}
		check("proxy_route", proxyState, summary)
		check("local_http", model.Skip, "Independent backend HTTP check not configured")
		if checks, ok := public[site.ID]; ok {
			r.Checks = append(r.Checks, checks...)
		} else {
			check("public_http", model.Unknown, "First public probe is in progress")
		}
		r.Overall = model.Overall(r.Checks)
		r.Diagnoses = diagnostics.Diagnose(r)
		reports = append(reports, r)
	}
	return reports
}

func probePublic(ctx context.Context, c config.Config, cached map[string][]model.Check, networkAt map[string]time.Time) publicResult {
	result := publicResult{map[string][]model.Check{}, map[string]time.Time{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, site := range c.Sites {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return result
		}
		wg.Add(1)
		go func(s config.Site) {
			defer wg.Done()
			defer func() { <-slots }()
			checks := []model.Check{}
			at := networkAt[s.ID]
			if time.Since(at) >= 5*time.Minute {
				checks = health.Network(ctx, s)
				at = time.Now()
			} else {
				for _, check := range cached[s.ID] {
					if check.Layer != "public_http" {
						checks = append(checks, check)
					}
				}
			}
			checks = append(checks, health.HTTP(ctx, s, s.Health.PublicURL, "public_http"))
			mu.Lock()
			result.checks[s.ID], result.networkAt[s.ID] = checks, at
			mu.Unlock()
		}(site)
	}
	wg.Wait()
	return result
}

func persistReports(ctx context.Context, c config.Config, store *storage.Store, reports []model.Report) error {
	previous, err := store.Latest(ctx, c.Host.ID)
	if err != nil {
		return err
	}
	if err = store.Reports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	if err = events.Record(ctx, store, previous, reports); err != nil {
		return err
	}
	for _, r := range incidents.GroupReports(c, reports, "agent") {
		if _, err = incidents.Observe(ctx, store, r, "agent", 3, 2); err != nil {
			return err
		}
	}
	return nil
}

func CollectOnce(ctx context.Context, c config.Config, store *storage.Store) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	path, err := collector.Bootstrap(ctx, c)
	if err != nil {
		return err
	}
	session, err := collector.Start(ctx, c, path)
	if err != nil {
		return err
	}
	defer session.Close()
	gotInventory, gotMetrics := false, false
	var registry discovery.Registry
	for !gotInventory || !gotMetrics {
		m, err := session.Next()
		if err != nil {
			return err
		}
		switch m.Kind {
		case "inventory":
			if m.Inventory == nil {
				return fmt.Errorf("missing inventory payload")
			}
			registry, err = store.ReconcileInventory(ctx, *m.Inventory)
			if err != nil {
				return err
			}
			gotInventory = true
		case "host_metrics":
			if m.Sample == nil {
				return fmt.Errorf("missing telemetry payload")
			}
			if _, err = store.Add(ctx, c.Host.ID, "", "host_metrics", m.Sample.ObservedAt, *m.Sample); err != nil {
				return err
			}
			if err = store.Transition(ctx, c.Host.ID, "", "host_metrics", *m.Sample, "", m.ObservedAt, nil); err != nil {
				return err
			}
			gotMetrics = true
		}
	}
	if err = connection(ctx, store, c.Host.ID, true, ""); err != nil {
		return err
	}
	resolved := discovery.ConfigSites(registry, c)
	public := probePublic(ctx, resolved, nil, nil)
	return persistReports(ctx, resolved, store, baseReports(resolved, registry, public.checks))
}

// RunLive owns the only collector session and schedules public checks independently
// of fast telemetry. Reconnection reuses the same protocol and reconciliation.
func RunLive(ctx context.Context, c config.Config, store *storage.Store, interval time.Duration, wake <-chan struct{}) error {
	startup, startCancel := context.WithTimeout(ctx, 45*time.Second)
	path, err := collector.Bootstrap(startup, c)
	startCancel()
	if err != nil {
		_ = connection(ctx, store, c.Host.ID, false, err.Error())
		return err
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err = stream(ctx, c, store, path, interval, wake)
		if ctx.Err() != nil {
			return nil
		}
		_ = connection(ctx, store, c.Host.ID, false, err.Error())
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(30*time.Second, backoff*2)
	}
	return nil
}

func stream(parent context.Context, c config.Config, store *storage.Store, path string, interval time.Duration, wake <-chan struct{}) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	session, err := collector.Start(ctx, c, path)
	if err != nil {
		return err
	}
	defer session.Close()
	frames := make(chan collector.Message, 4)
	failures := make(chan error, 1)
	go func() {
		for {
			m, err := session.Next()
			if err != nil {
				select {
				case failures <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case frames <- m:
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	results := make(chan publicResult, 1)
	busy := false
	registry, _ := store.Inventory(ctx, c.Host.ID)
	resolved := discovery.ConfigSites(registry, c)
	public := map[string][]model.Check{}
	networkAt := map[string]time.Time{}
	lastHistory := time.Time{}
	lastRetain := time.Time{}
	launch := func() {
		if busy || len(resolved.Sites) == 0 {
			return
		}
		busy = true
		current := resolved
		cached := public
		at := networkAt
		go func() {
			result := probePublic(ctx, current, cached, at)
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}()
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-failures:
			return err
		case <-wake:
			if err := session.Refresh(); err != nil {
				return err
			}
			launch()
		case <-ticker.C:
			launch()
		case result := <-results:
			busy = false
			public, networkAt = result.checks, result.networkAt
			if err := persistReports(ctx, resolved, store, baseReports(resolved, registry, public)); err != nil {
				return err
			}
		case message := <-frames:
			switch message.Kind {
			case "hello":
				if err := connection(ctx, store, c.Host.ID, true, ""); err != nil {
					return err
				}
			case "inventory":
				if message.Inventory == nil || message.Inventory.HostID != c.Host.ID {
					return fmt.Errorf("invalid inventory payload")
				}
				registry, err = store.ReconcileInventory(ctx, *message.Inventory)
				if err != nil {
					return err
				}
				resolved = discovery.ConfigSites(registry, c)
				if err = persistReports(ctx, resolved, store, baseReports(resolved, registry, public)); err != nil {
					return err
				}
				launch()
			case "host_metrics":
				if message.Sample == nil || message.Sample.HostID != c.Host.ID {
					return fmt.Errorf("invalid host telemetry")
				}
				sample := *message.Sample
				if err = store.Transition(ctx, c.Host.ID, "", "host_metrics", sample, "", sample.ObservedAt, nil); err != nil {
					return err
				}
				if time.Since(lastHistory) >= 15*time.Second {
					if _, err = store.Add(ctx, c.Host.ID, "", "host_metrics", sample.ObservedAt, sample); err != nil {
						return err
					}
					lastHistory = time.Now()
				}
			case "workload_metrics":
				for _, sample := range message.Samples {
					for _, site := range registry.Sites {
						for _, id := range site.WorkloadIDs {
							if id == sample.SiteID {
								sample.SiteID = site.ID
								for _, s := range resolved.Sites {
									if s.Proxy.Domain == site.Domain {
										sample.SiteID = s.ID
									}
								}
								if err = store.Transition(ctx, c.Host.ID, sample.SiteID, "metrics", sample, "", sample.ObservedAt, nil); err != nil {
									return err
								}
								break
							}
						}
					}
				}
			}
			if time.Since(lastRetain) >= time.Hour {
				if err = store.Retain(ctx, 90*24*time.Hour); err != nil {
					return err
				}
				lastRetain = time.Now()
			}
		}
	}
}
