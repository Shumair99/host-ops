package agent

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/changes"
	"github.com/Shumair99/host-ops/internal/collector"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/events"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"net"
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
		for _, id := range item.WorkloadIDs {
			w := workloads[id]
			state := model.Pass
			if w.Status != "running" && w.Status != "active" {
				state = model.Fail
			}
			if sources[w.SourceID].State != "ready" {
				state = model.Unknown
			}
			check("runtime", state, w.Name+": "+w.Status)
			if len(item.WorkloadIDs) == 1 {
				r.Runtime.Name = w.Name
				r.Runtime.Type = w.Type
				r.Runtime.Status = w.Status
				r.Runtime.Health = w.Health
				r.Runtime.Image = w.Image
				r.Runtime.ImageID, r.Runtime.RestartCount, r.Runtime.StartedAt = w.ImageID, w.RestartCount, w.StartedAt
				r.Runtime.Commit = w.Revision
				r.Runtime.RestartPolicy = w.RestartPolicy
				r.Runtime.IPs = map[string]string{}
				for n, net := range w.Networks {
					r.Runtime.IPs[n] = net.IP
				}
				r.Runtime.Ports = map[string][]model.Port{}
				for port, bindings := range w.Ports {
					for _, binding := range bindings {
						ip, hostPort, _ := net.SplitHostPort(binding)
						r.Runtime.Ports[port] = append(r.Runtime.Ports[port], model.Port{HostIP: ip, HostPort: hostPort})
					}
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
		if item.State == "unresolved" && len(item.WorkloadIDs) > 0 {
			check("runtime", model.Unknown, "Some route backends could not be mapped to workloads")
		}
		proxyState, summary := model.Warn, "Declared route found; loaded state unverified"
		proxyChecks := map[string]bool{}
		for _, route := range item.Routes {
			source := sources[route.SourceID]
			if route.ProxyID != "" && !proxyChecks[route.ProxyID] {
				proxyChecks[route.ProxyID] = true
				state, text := model.Unknown, "Proxy runtime observation unavailable"
				if w, ok := workloads[route.ProxyID]; ok && sources[w.SourceID].State == "ready" {
					state, text = model.Fail, w.Name+": "+w.Status
					if w.Status == "running" || w.Status == "active" {
						state = model.Pass
					}
				}
				r.Checks = append(r.Checks, model.Check{HostID: c.Host.ID, SiteID: site.ID, Layer: "proxy", Dependency: route.ProxyID, State: state, Summary: text})
			}
			if source.State == "ready" && source.Mode == "active" {
				proxyState, summary = model.Pass, "Domain present in active proxy configuration"
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
	if err = changes.Record(ctx, store, reports); err != nil {
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
	if err := store.Prune(ctx, c.History); err != nil {
		return err
	}
	var err error
	c, err = collectorConfig(ctx, c, store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	path, err := collector.Bootstrap(ctx, c)
	if err != nil {
		return err
	}
	session, err := collector.Start(ctx, c, path, false)
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
	// Long public probes must not leave a one-shot dashboard with old telemetry.
	for {
		message, readErr := session.Next()
		if readErr != nil {
			return readErr
		}
		if message.Kind == "host_metrics" && message.Sample != nil && time.Since(message.Sample.ObservedAt) < 2*time.Second {
			if err = store.Transition(ctx, c.Host.ID, "", "host_metrics", *message.Sample, "", message.ObservedAt, nil); err != nil {
				return err
			}
			break
		}
	}
	if err := persistReports(ctx, resolved, store, baseReports(resolved, registry, public.checks)); err != nil {
		return err
	}
	return store.Prune(ctx, c.History)
}

// RunLive owns the only collector session and schedules public checks independently
// of fast telemetry. Reconnection reuses the same protocol and reconciliation.
func RunLive(ctx context.Context, c config.Config, store *storage.Store, interval time.Duration, wake <-chan struct{}) error {
	if interval < 5*time.Second {
		return fmt.Errorf("public check interval must be at least 5s")
	}
	if err := connection(ctx, store, c.Host.ID, false, "Connecting to collector"); err != nil {
		return err
	}
	path := ""
	var err error
	backoff := time.Second
	for ctx.Err() == nil {
		if path == "" {
			startup, startCancel := context.WithTimeout(ctx, 45*time.Second)
			path, err = collector.Bootstrap(startup, c)
			startCancel()
		}
		if err == nil {
			started := time.Now()
			err = stream(ctx, c, store, path, interval, wake)
			if time.Since(started) >= 30*time.Second {
				backoff = time.Second
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		if writeErr := connection(ctx, store, c.Host.ID, false, err.Error()); writeErr != nil {
			return fmt.Errorf("persist disconnected state: %w", writeErr)
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(30*time.Second, backoff*2)
		if path != "" {
			err = nil
		}
	}
	return nil
}

func stream(parent context.Context, c config.Config, store *storage.Store, path string, interval time.Duration, wake <-chan struct{}) error {
	if err := store.Prune(parent, c.History); err != nil {
		return err
	}
	var err error
	c, err = collectorConfig(parent, c, store)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	session, err := collector.Start(ctx, c, path, c.Host.HistoryMode == "continuous")
	if err != nil {
		return err
	}
	defer session.Close()
	type incoming struct {
		message collector.Message
		source  string
	}
	frames := make(chan incoming, 8)
	failures := make(chan error, 2)
	readStream := func(source string, current *collector.Session) {
		for {
			m, err := current.Next()
			if err != nil {
				select {
				case failures <- err:
				case <-ctx.Done():
				}
				return
			}
			select {
			case frames <- incoming{message: m, source: source}:
			case <-ctx.Done():
				return
			}
		}
	}
	go readStream("collector", session)
	var historySession *collector.Session
	if c.Host.HistoryMode == "continuous" {
		historySession, err = collector.StartPersistent(ctx, c)
		if err != nil {
			return err
		}
		defer historySession.Close()
		go readStream("persistent", historySession)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	results := make(chan publicResult, 1)
	busy := false
	registry, _ := store.Inventory(ctx, c.Host.ID)
	resolved := discovery.ConfigSites(registry, c)
	public := map[string][]model.Check{}
	networkAt := map[string]time.Time{}
	lastHistory := time.Time{}
	lastWorkloadHistory := time.Time{}
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
		case frame := <-frames:
			message := frame.message
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
				if err = refreshReports(ctx, resolved, store, baseReports(resolved, registry, public)); err != nil {
					return err
				}
				launch()
			case "host_metrics":
				if frame.source == "collector" && c.Host.HistoryMode == "continuous" {
					continue
				}
				if message.Sample == nil || message.Sample.HostID != c.Host.ID {
					return fmt.Errorf("invalid host telemetry")
				}
				sample := *message.Sample
				if err = store.Transition(ctx, c.Host.ID, "", "host_metrics", sample, "", sample.ObservedAt, nil); err != nil {
					return err
				}
				if time.Since(lastHistory) >= 15*time.Second {
					if _, err = store.AddUnique(ctx, c.Host.ID, "", "host_metrics", sample.ObservedAt, sample); err != nil {
						return err
					}
					lastHistory = time.Now()
				}
			case "history":
				if frame.source != "persistent" || len(message.Samples) > 128 {
					return fmt.Errorf("invalid persistent host history")
				}
				for _, sample := range message.Samples {
					if sample.HostID != c.Host.ID || sample.SiteID != "" {
						return fmt.Errorf("persistent history host mismatch")
					}
					if _, err = store.AddUnique(ctx, c.Host.ID, "", "host_metrics", sample.ObservedAt, sample); err != nil {
						return err
					}
				}
				if len(message.Samples) > 0 {
					latest := message.Samples[len(message.Samples)-1]
					if err = store.Transition(ctx, c.Host.ID, "", "host_metrics", latest, "", latest.ObservedAt, nil); err != nil {
						return err
					}
				}
			case "workload_metrics":
				historyDue := time.Since(lastWorkloadHistory) >= 15*time.Second
				for _, sample := range siteSamples(registry, resolved, message.Samples) {
					if err = store.Transition(ctx, c.Host.ID, sample.SiteID, "metrics", sample, "", sample.ObservedAt, nil); err != nil {
						return err
					}
					if historyDue && !sample.ObservedAt.IsZero() {
						if _, err = store.Add(ctx, c.Host.ID, sample.SiteID, "metrics", sample.ObservedAt, sample); err != nil {
							return err
						}
					}
				}
				if historyDue {
					lastWorkloadHistory = time.Now()
				}
			}
			if time.Since(lastRetain) >= time.Minute {
				if err = store.Prune(ctx, c.History); err != nil {
					return err
				}
				lastRetain = time.Now()
			}
		}
	}
}

// Keep previously observed route owners in filtered service scans so a stopped
// service remains observable after its listeners/process have disappeared.
func collectorConfig(ctx context.Context, c config.Config, store *storage.Store) (config.Config, error) {
	registry, err := store.Inventory(ctx, c.Host.ID)
	if err != nil {
		return c, err
	}
	owners := map[string]bool{}
	for _, site := range registry.Sites {
		if site.State == "removed" {
			continue
		}
		for _, id := range site.WorkloadIDs {
			owners[id] = true
		}
		for _, route := range site.Routes {
			owners[route.ProxyID] = true
		}
	}
	units := map[string]bool{}
	c.Host.ServiceUnits = append([]string(nil), c.Host.ServiceUnits...)
	for _, unit := range c.Host.ServiceUnits {
		units[unit] = true
	}
	for _, w := range registry.Snapshot.Workloads {
		if w.Type == "systemd" && owners[w.ID] && !units[w.Name] {
			c.Host.ServiceUnits = append(c.Host.ServiceUnits, w.Name)
			units[w.Name] = true
		}
	}
	return c, nil
}

func siteSamples(registry discovery.Registry, c config.Config, samples []metrics.Sample) []metrics.Sample {
	byID := map[string]metrics.Sample{}
	for _, sample := range samples {
		byID[sample.SiteID] = sample
	}
	out := []metrics.Sample{}
	for _, site := range registry.Sites {
		if site.State == "removed" || len(site.WorkloadIDs) == 0 {
			continue
		}
		id := site.ID
		for _, s := range c.Sites {
			if s.Proxy.Domain == site.Domain {
				id = s.ID
			}
		}
		combined := metrics.Sample{HostID: c.Host.ID, SiteID: id, Values: map[string]float64{}, Unavailable: []string{}}
		for _, workloadID := range site.WorkloadIDs {
			sample, ok := byID[workloadID]
			if !ok {
				combined.Unavailable = append(combined.Unavailable, "Backend metric not available: "+workloadID)
				continue
			}
			combined.Unavailable = append(combined.Unavailable, sample.Unavailable...)
			if combined.ObservedAt.IsZero() || sample.ObservedAt.Before(combined.ObservedAt) {
				combined.ObservedAt = sample.ObservedAt
			}
			for key, value := range sample.Values {
				if len(site.WorkloadIDs) == 1 {
					combined.Values[key] = value
				} else if key == "cpu_percent" || key == "memory_used_bytes" || key == "network_rx_bytes" || key == "network_tx_bytes" {
					combined.Values[key] += value
				}
			}
		}
		out = append(out, combined)
	}
	return out
}

func refreshReports(ctx context.Context, c config.Config, store *storage.Store, reports []model.Report) error {
	previous, err := store.Latest(ctx, c.Host.ID)
	if err != nil {
		return err
	}
	if err = store.UpdateReports(ctx, c.Host.ID, reports); err != nil {
		return err
	}
	if err = events.Record(ctx, store, previous, reports); err != nil {
		return err
	}
	return changes.Record(ctx, store, reports)
}
