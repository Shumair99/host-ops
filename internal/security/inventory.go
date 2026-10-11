package security

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"net"
	"time"
)

func InspectInventory(ctx context.Context, c config.Config, reports []model.Report, reg discovery.Registry, runner platform.Runner) Report {
	reports = append([]model.Report(nil), reports...)
	ready := map[string]bool{}
	for _, source := range reg.Snapshot.Sources {
		ready[source.ID] = source.State == "ready"
	}
	for _, w := range reg.Snapshot.Workloads {
		if w.Type != "docker" || !ready[w.SourceID] || !model.Fresh(reg.Snapshot.ObservedAt, time.Now(), model.DefaultMaxAge) {
			continue
		}
		r := model.Runtime{Name: w.Name, Type: w.Type, Status: w.Status, PostureObserved: w.PostureObserved, Processes: w.Processes, User: w.User, Privileged: w.Privileged, ReadOnly: w.ReadOnly, MemoryLimit: w.MemoryLimit, Image: w.Image}
		for port, bindings := range w.Ports {
			for _, binding := range bindings {
				host, portValue, err := net.SplitHostPort(binding)
				if err == nil {
					if r.Ports == nil {
						r.Ports = map[string][]model.Port{}
					}
					r.Ports[port] = append(r.Ports[port], model.Port{HostIP: host, HostPort: portValue})
				}
			}
		}
		for _, mount := range w.Mounts {
			r.Mounts = append(r.Mounts, model.Mount{Source: mount.Source, Destination: mount.Destination})
		}
		reports = append(reports, model.Report{SiteID: "container:" + w.Name, ObservedAt: reg.Snapshot.ObservedAt, Runtime: r})
	}
	return Inspect(ctx, c, reports, runner)
}
