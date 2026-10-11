package agent

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestAutomaticReportsKeepStoppedBackend(t *testing.T) {
	snapshot := discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []discovery.Source{{ID: "runtime", State: "ready"}, {ID: "proxy", State: "ready", Mode: "active"}}, Workloads: []discovery.Workload{{ID: "w", SourceID: "runtime", Type: "docker", Name: "web", Status: "exited", Networks: map[string]discovery.Network{"docker:shared": {Aliases: []string{"web"}}}}}, Routes: []discovery.Route{{ID: "r", SourceID: "proxy", Hosts: []string{"example.com"}, Kind: "proxy", Scheme: "https", Upstreams: []string{"web:80"}, Networks: []string{"docker:shared"}}}}
	registry := discovery.Reconcile(snapshot, discovery.Registry{})
	c := discovery.ConfigSites(registry, config.Config{Host: config.Host{ID: "h"}})
	reports := baseReports(c, registry, map[string][]model.Check{})
	if len(reports) != 1 || reports[0].Overall != "critical" {
		t.Fatalf("stopped site was hidden or green: %+v", reports)
	}
	if len(c.Sites) != 1 || !c.Sites[0].Discovered || !c.Sites[0].Health.Automatic {
		t.Fatal("site required manual registration")
	}
}

func TestSharedBackendMetricsReachEachDomainWithoutMutatingSamples(t *testing.T) {
	at := time.Now().Add(-time.Second)
	registry := discovery.Registry{Sites: []discovery.Site{{ID: "a", Domain: "a.example", WorkloadIDs: []string{"w"}}, {ID: "b", Domain: "b.example", WorkloadIDs: []string{"w"}}}}
	samples := []metrics.Sample{{SiteID: "w", ObservedAt: at, Values: map[string]float64{"cpu_percent": 12, "memory_percent": 30}}}
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "custom", Proxy: config.Proxy{Domain: "a.example"}}}}
	got := siteSamples(registry, c, samples)
	if len(got) != 2 || got[0].SiteID != "custom" || got[1].SiteID != "b" || got[0].Values["cpu_percent"] != 12 || got[1].Values["cpu_percent"] != 12 || !got[0].ObservedAt.Equal(at) || samples[0].SiteID != "w" {
		t.Fatalf("shared backend was lost or given a false freshness timestamp: %+v", got)
	}
}

func TestMultipleBackendMetricsSumCompatibleUnitsAndShowMissingEvidence(t *testing.T) {
	registry := discovery.Registry{Sites: []discovery.Site{{ID: "s", WorkloadIDs: []string{"w1", "w2", "missing"}}}}
	samples := []metrics.Sample{{SiteID: "w1", Values: map[string]float64{"cpu_percent": 12, "memory_used_bytes": 100, "memory_percent": 50}}, {SiteID: "w2", Values: map[string]float64{"cpu_percent": 8, "memory_used_bytes": 200, "memory_percent": 10}}}
	got := siteSamples(registry, config.Config{}, samples)
	if len(got) != 1 || got[0].Values["cpu_percent"] != 20 || got[0].Values["memory_used_bytes"] != 300 || len(got[0].Unavailable) != 1 {
		t.Fatal(got)
	}
	if _, ok := got[0].Values["memory_percent"]; ok {
		t.Fatal("percentages with different limits were added")
	}
}

func TestPartiallyResolvedSiteCannotBeHealthy(t *testing.T) {
	registry := discovery.Registry{Sites: []discovery.Site{{Domain: "a.example", State: "unresolved", WorkloadIDs: []string{"w"}}}, Snapshot: discovery.Snapshot{Sources: []discovery.Source{{ID: "runtime", State: "ready"}}, Workloads: []discovery.Workload{{ID: "w", SourceID: "runtime", Name: "web", Status: "running"}}}}
	c := config.Config{Sites: []config.Site{{ID: "s", Proxy: config.Proxy{Domain: "a.example"}}}}
	got := baseReports(c, registry, map[string][]model.Check{"s": {{Layer: "public_http", State: model.Pass}}})
	found := false
	for _, check := range got[0].Checks {
		found = found || check.Layer == "runtime" && check.State == model.Unknown
	}
	if !found || got[0].Overall == "healthy" {
		t.Fatal("a missing backend was concealed by a working backend")
	}
}

func TestDiscoveredReportRetainsHardeningEvidenceAndSocketMounts(t *testing.T) {
	w := discovery.Workload{ID: "w", Type: "docker", SourceID: "runtime", Name: "app", Status: "running", PostureObserved: true, User: "1000:1000", MemoryLimit: 536870912, ReadOnly: true, Privileged: true, Mounts: []discovery.Mount{{Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock"}}}
	registry := discovery.Registry{Sites: []discovery.Site{{Domain: "example.com", WorkloadIDs: []string{"w"}}}, Snapshot: discovery.Snapshot{Workloads: []discovery.Workload{w}, Sources: []discovery.Source{{ID: "runtime", State: "ready"}}}}
	c := config.Config{Sites: []config.Site{{ID: "site", Proxy: config.Proxy{Domain: "example.com"}, Runtime: config.Runtime{Type: "docker", Container: "app"}}}}
	r := baseReports(c, registry, nil)[0].Runtime
	if !r.PostureObserved || r.User != w.User || r.MemoryLimit != w.MemoryLimit || !r.ReadOnly || !r.Privileged || len(r.Mounts) != 1 || r.Mounts[0].Destination != "/var/run/docker.sock" {
		t.Fatal("discovery lost security evidence", r)
	}
	registry.Snapshot.Sources[0].State = "unavailable"
	if r = baseReports(c, registry, nil)[0].Runtime; r.PostureObserved {
		t.Fatal("retained metadata from an unavailable source was treated as observed")
	}
}
