package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/discovery"
)

type statsFixture struct{ t *testing.T }

func (f statsFixture) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "podman" || len(args) < 5 || !strings.Contains(args[3], ".CPUPerc") || !strings.Contains(args[3], ".Name") {
		f.t.Fatal("runtime did not use the shared stats projection", name, args)
	}
	return []byte(`{"Name":"web","CPUPerc":"--","MemPerc":"25%","MemUsage":"4MiB / 16MiB","NetIO":"-- / --"}`), nil
}

func TestStatsKeepPartialValuesAndMissingRootlessCounters(t *testing.T) {
	snapshot := discovery.Snapshot{HostID: "h", Workloads: []discovery.Workload{{ID: "w", Name: "web", Type: "podman", Status: "running"}}}
	got := workloadStats(context.Background(), statsFixture{t}, snapshot)
	if len(got) != 1 || got[0].SiteID != "w" || got[0].Values["memory_used_bytes"] != 4*1024*1024 || len(got[0].Unavailable) != 3 {
		t.Fatal(got)
	}
	if _, ok := got[0].Values["cpu_percent"]; ok {
		t.Fatal("unavailable CPU became a measured zero")
	}
}
