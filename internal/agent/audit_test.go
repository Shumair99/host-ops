package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestDiscoveryProxyFailureOpensOneSharedIncidentAndRecovers(t *testing.T) {
	for _, sourceState := range []string{"ready", "unavailable"} {
		t.Run(sourceState, func(t *testing.T) {
			s, err := storage.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			registry := discovery.Registry{Snapshot: discovery.Snapshot{Sources: []discovery.Source{{ID: "runtime", State: sourceState}, {ID: "p", State: "unavailable"}}, Workloads: []discovery.Workload{{ID: "proxy", SourceID: "runtime", Name: "caddy", Status: "exited"}}}}
			c := config.Config{Host: config.Host{ID: "h"}}
			public := map[string][]model.Check{}
			for _, domain := range []string{"a.example", "b.example"} {
				registry.Sites = append(registry.Sites, discovery.Site{ID: domain, Domain: domain, State: "stale", Routes: []discovery.Route{{ProxyID: "proxy", SourceID: "p", Kind: "response"}}})
				c.Sites = append(c.Sites, config.Site{ID: domain, HostID: "h", Proxy: config.Proxy{Domain: domain}})
				public[domain] = []model.Check{{Layer: "public_http", State: model.Unknown}}
			}
			for i := 0; i < 3; i++ {
				if err := persistReports(context.Background(), c, s, baseReports(c, registry, public)); err != nil {
					t.Fatal(err)
				}
			}
			list, err := incidents.List(context.Background(), s, "h", "")
			if err != nil {
				t.Fatal(err)
			}
			if sourceState == "unavailable" {
				if len(list) != 0 {
					t.Fatal("unreadable runtime inferred as outage", list)
				}
				return
			}
			var opened incidents.Incident
			if len(list) == 1 {
				json.Unmarshal(list[0].Data, &opened)
			}
			if len(list) != 1 || opened.Dependency != "proxy" || len(opened.SiteIDs) != 2 || opened.Status != "open" {
				t.Fatal(list)
			}
			registry.Snapshot.Workloads[0].Status = "running"
			for i := 0; i < 2; i++ {
				if err := persistReports(context.Background(), c, s, baseReports(c, registry, public)); err != nil {
					t.Fatal(err)
				}
			}
			list, _ = incidents.List(context.Background(), s, "h", "")
			var resolved incidents.Incident
			if len(list) == 1 {
				json.Unmarshal(list[0].Data, &resolved)
			}
			if len(list) != 1 || resolved.Status != "resolved" {
				t.Fatal("shared incident failed recovery", list)
			}
		})
	}
}

func TestMissingWorkloadMetricsHaveNoInventedObservationTime(t *testing.T) {
	registry := discovery.Registry{Sites: []discovery.Site{{ID: "s", WorkloadIDs: []string{"missing"}}}}
	got := siteSamples(registry, config.Config{}, nil)
	if len(got) != 1 || !got[0].ObservedAt.IsZero() || len(got[0].Values) != 0 || len(got[0].Unavailable) != 1 {
		t.Fatal(got)
	}
}

func TestMultipleBackendsHaveNoArbitraryPassport(t *testing.T) {
	registry := discovery.Registry{Sites: []discovery.Site{{Domain: "example.com", WorkloadIDs: []string{"a", "b"}}}, Snapshot: discovery.Snapshot{Sources: []discovery.Source{{ID: "runtime", State: "ready"}}, Workloads: []discovery.Workload{{ID: "a", SourceID: "runtime", Name: "first", Type: "docker", Status: "running", Image: "first:v1"}, {ID: "b", SourceID: "runtime", Name: "second", Type: "docker", Status: "running", Image: "second:v1"}}}}
	c := config.Config{Sites: []config.Site{{ID: "s", Runtime: config.Runtime{Type: "unresolved"}, Proxy: config.Proxy{Domain: "example.com"}}}}
	r := baseReports(c, registry, nil)[0]
	if r.Runtime.Name != "" || r.Runtime.Image != "" || r.ObservedAt.IsZero() || time.Since(r.ObservedAt) > time.Minute {
		t.Fatal(r.Runtime)
	}
}

func TestLivePersistenceDoesNotDuplicateUnchangedHealthAndSnapshots(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	c := config.Config{Host: config.Host{ID: "h"}}
	start := time.Now().Add(-10 * time.Minute)
	r := model.Report{HostID: "h", SiteID: "s", Overall: "healthy", Runtime: model.Runtime{Status: "running"}, Checks: []model.Check{{Layer: "public_http", State: model.Pass, StatusCode: 200}}}
	for cycle := 0; cycle < 20; cycle++ {
		r.ObservedAt = start.Add(time.Duration(cycle) * 30 * time.Second)
		r.Checks[0].LatencyMS = float64(100 + cycle)
		r.Checks[0].Summary = fmt.Sprintf("HTTP 200; %.0f ms", r.Checks[0].LatencyMS)
		if err := persistReports(ctx, c, s, []model.Report{r}); err != nil {
			t.Fatal(err)
		}
		r.ObservedAt = r.ObservedAt.Add(time.Second)
		if err := refreshReports(ctx, c, s, []model.Report{r}); err != nil {
			t.Fatal(err)
		}
	}
	health, err := s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := s.Query(ctx, "h", "s", "snapshot", time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(health) != 10 || len(snapshots) != 1 {
		t.Fatal("live writers bypassed throttling", len(health), len(snapshots))
	}
	r.Overall = "critical"
	r.Checks[0].State = model.Fail
	r.Checks[0].StatusCode = 500
	r.Checks[0].Summary = "HTTP 500; 120 ms"
	if err := persistReports(ctx, c, s, []model.Report{r}); err != nil {
		t.Fatal(err)
	}
	snapshots, _ = s.Query(ctx, "h", "s", "snapshot", time.Time{}, 100)
	changes, _ := s.Query(ctx, "h", "s", "change", time.Time{}, 100)
	if len(snapshots) != 2 || len(changes) != 1 {
		t.Fatal("real state change was lost", len(snapshots), len(changes))
	}
}

func TestReconnectKeepsKnownSystemdRouteOwnersInFilteredScan(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	registry := discovery.Registry{Sites: []discovery.Site{{Domain: "example.com", Routes: []discovery.Route{{ProxyID: "proxy"}}, WorkloadIDs: []string{"app"}}}, Snapshot: discovery.Snapshot{Workloads: []discovery.Workload{{ID: "proxy", Type: "systemd", Name: "custom-proxy.service"}, {ID: "app", Type: "systemd", Name: "app.service"}, {ID: "noise", Type: "systemd", Name: "apt-daily.service"}}}}
	if err := s.Transition(ctx, "h", "", "inventory", registry, "", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	c, err := collectorConfig(ctx, config.Config{Host: config.Host{ID: "h"}}, s)
	if err != nil || len(c.Host.ServiceUnits) != 2 {
		t.Fatal("known owner evidence lost or noise retained", c.Host.ServiceUnits, err)
	}
}
