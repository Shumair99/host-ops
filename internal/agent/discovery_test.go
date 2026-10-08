package agent

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
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
