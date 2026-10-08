package collector

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/platform"
	"strings"
	"time"
)

func workloadStats(ctx context.Context, runner platform.Runner, snapshot discovery.Snapshot) []metrics.Sample {
	out := []metrics.Sample{}
	for _, engine := range []string{"docker", "podman"} {
		byName := map[string]discovery.Workload{}
		names := []string{}
		for _, w := range snapshot.Workloads {
			if w.Type == engine && w.Status == "running" {
				byName[w.Name] = w
				names = append(names, w.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		format := `{"Name":{{json .Name}},"CPUPerc":{{json .CPUPerc}},"MemPerc":{{json .MemPerc}},"MemUsage":{{json .MemUsage}},"NetIO":{{json .NetIO}}}`
		data, err := runner.Run(ctx, engine, append([]string{"stats", "--no-stream", "--format", format, "--"}, names...)...)
		if err != nil {
			for _, name := range names {
				out = append(out, metrics.Sample{HostID: snapshot.HostID, SiteID: byName[name].ID, ObservedAt: time.Now().UTC(), Values: map[string]float64{}, Unavailable: []string{"Workload metrics unavailable"}})
			}
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			var fields map[string]any
			if json.Unmarshal([]byte(line), &fields) != nil {
				continue
			}
			name, _ := fields["Name"].(string)
			if name == "" {
				name, _ = fields["Container"].(string)
			}
			w, ok := byName[name]
			if !ok {
				continue
			}
			sample := metrics.Sample{HostID: snapshot.HostID, SiteID: w.ID, ObservedAt: time.Now().UTC(), Values: map[string]float64{}, Unavailable: []string{}}
			sample.Values, sample.Unavailable = metrics.ParseStats([]byte(line))
			out = append(out, sample)
		}
	}
	return out
}
