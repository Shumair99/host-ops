package collector

import (
	"context"
	"errors"
	"github.com/Shumair99/host-ops/internal/discovery"
	"strings"
	"testing"
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

type statsRunner struct {
	fail  bool
	calls []string
}

func (r *statsRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.fail {
		return nil, errors.New("access denied")
	}
	return []byte(`{"Name":"web","CPUPerc":"2%","MemPerc":"10%","MemUsage":"10MiB / 100MiB"}` + "\n" + `{"Name":"unrelated","CPUPerc":"99%"}` + "\ninvalid"), nil
}
func TestWorkloadStatsKeepsIdentityAndReportsUnavailable(t *testing.T) {
	s := discovery.Snapshot{HostID: "h", Workloads: []discovery.Workload{{ID: "w", Name: "web", Type: "docker", Status: "running"}, {ID: "stopped", Name: "stopped", Type: "docker", Status: "exited"}}}
	for _, fail := range []bool{false, true} {
		runner := &statsRunner{fail: fail}
		samples := workloadStats(context.Background(), runner, s)
		if len(samples) != 1 || samples[0].SiteID != "w" || samples[0].HostID != "h" || samples[0].ObservedAt.IsZero() {
			t.Fatal(samples)
		}
		if fail {
			if len(samples[0].Unavailable) == 0 || len(samples[0].Values) != 0 {
				t.Fatal(samples)
			}
		} else if samples[0].Values["cpu_percent"] != 2 {
			t.Fatal(samples)
		}
		if len(runner.calls) != 1 || strings.Contains(runner.calls[0], "stopped") {
			t.Fatal(runner.calls)
		}
	}
}
