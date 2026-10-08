package collector

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"os"
	"runtime"
	"strings"
	"time"
)

func Scan(ctx context.Context, host config.Host) discovery.Snapshot {
	snapshot := discovery.Snapshot{HostID: host.ID, ObservedAt: time.Now().UTC(), OS: runtime.GOOS, Architecture: runtime.GOARCH, Sources: []discovery.Source{}, Workloads: []discovery.Workload{}, Routes: []discovery.Route{}}
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				snapshot.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "host", Kind: "linux", State: "ready", Mode: "active", ObservedAt: snapshot.ObservedAt})
	return snapshot
}
