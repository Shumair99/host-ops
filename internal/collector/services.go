package collector

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"os/exec"
)

func readServices(ctx context.Context, runner platform.Runner, snapshot *discovery.Snapshot) {
	source := discovery.Source{ID: "linux/services", Kind: "systemd", Mode: "active", State: "ready", ObservedAt: snapshot.ObservedAt}
	if _, err := exec.LookPath("systemctl"); err != nil {
		source.State = "absent"
		snapshot.Sources = append(snapshot.Sources, source)
		return
	}
	data, err := runner.Run(ctx, "systemctl", "list-units", "--all", "--type=service", "--no-pager", "--output=json")
	if err != nil {
		source.State, source.Issue = "unavailable", "Service manager inventory unreadable"
		snapshot.Sources = append(snapshot.Sources, source)
		return
	}
	var units []struct{ Unit, Active, Sub string }
	if json.Unmarshal(data, &units) != nil {
		source.State, source.Issue = "unavailable", "Service manager JSON not supported"
	} else {
		for _, unit := range units {
			snapshot.Workloads = append(snapshot.Workloads, discovery.Workload{ID: discovery.StableID("workload", snapshot.HostID+"|systemd|"+unit.Unit), SourceID: source.ID, Type: "systemd", Name: unit.Unit, Status: unit.Active})
		}
	}
	snapshot.Sources = append(snapshot.Sources, source)
}
