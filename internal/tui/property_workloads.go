package tui

import (
	"fmt"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
)

func siteLabel(s config.Site) string {
	if s.Proxy.Domain != "" {
		return clean(s.Proxy.Domain)
	}
	if s.Name != "" {
		return clean(s.Name)
	}
	return clean(s.ID)
}

func propertyLabel(g siteGroup) string {
	label := siteLabel(g.Primary)
	if len(g.Children) > 0 {
		label += fmt.Sprintf(" +%d", len(g.Children))
	}
	return label
}

func workloadIdentity(s config.Site) string {
	identity := ""
	switch s.Runtime.Type {
	case "docker":
		identity = s.Runtime.Container
	case "systemd":
		identity = s.Runtime.Unit
	case "static":
		identity = s.Runtime.Directory
	}
	if identity == "" {
		return s.HostID + "/site/" + s.ID
	}
	return s.HostID + "/" + s.Runtime.Type + "/" + identity
}

type workloadReading struct {
	Sample metrics.Sample
	Label  string
}

// Sharing a property does not combine resources. Only references to the same
// actual workload are deduplicated, using its newest available observation.
func (m dashboard) workloadReadings(sites []config.Site) []workloadReading {
	out := []workloadReading{}
	indices := map[string]int{}
	for _, g := range groupSites(sites) {
		for _, s := range g.domains() {
			sample := m.snapshot.Workloads[s.ID]
			if (s.Runtime.Type == "" || s.Runtime.Type == "external") && len(sample.Values) == 0 {
				continue // Domain-only observations are not measured workloads.
			}
			identity := workloadIdentity(s)
			if index, exists := indices[identity]; exists {
				if sample.ObservedAt.After(out[index].Sample.ObservedAt) || (sample.ObservedAt.Equal(out[index].Sample.ObservedAt) && len(out[index].Sample.Values) == 0) {
					out[index].Sample = sample
				}
				continue
			}
			label := siteLabel(s)
			if s.ID != g.Primary.ID && s.Proxy.Domain != "" {
				label = clean(s.Proxy.Domain)
			}
			indices[identity] = len(out)
			out = append(out, workloadReading{Sample: sample, Label: label})
		}
	}
	return out
}

// Resource sorting ranks the busiest member rather than inventing an aggregate.
func (m dashboard) propertyMetric(id, key string) *float64 {
	var peak *float64
	for _, r := range m.workloadReadings(groupFor(m.rawSites(), id).domains()) {
		if n := value(r.Sample.Values, key); n != nil && (peak == nil || *n > *peak) {
			peak = n
		}
	}
	return peak
}

func (m dashboard) propertyWorkloadRows(g siteGroup, w int) []string {
	rows := []string{}
	for _, r := range m.workloadReadings(g.domains()) {
		rows = append(rows, clip(m.theme.strong(r.Label, ink), w-4))
		rows = append(rows, "  CPU "+percent(value(r.Sample.Values, "cpu_percent"))+"  Memory "+signalValue(value(r.Sample.Values, "memory_used_bytes"), "B"))
	}
	return rows
}
