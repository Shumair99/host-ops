package costs

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"math"
	"testing"
)

func TestMissingPropertyDoesNotEraseMeasuredResourceRatios(t *testing.T) {
	c := config.Config{Host: config.Host{MonthlyCost: 100}, Sites: []config.Site{{ID: "a"}, {ID: "b"}, {ID: "missing"}}}
	r := Allocate(c, map[string][]metrics.Sample{"a": {{Values: map[string]float64{"cpu_percent": 1, "memory_used_bytes": 1}}}, "b": {{Values: map[string]float64{"cpu_percent": 3, "memory_used_bytes": 3}}}})
	if math.Abs(r.Sites[0].CPUshare-1.0/6) > 1e-9 || math.Abs(r.Sites[1].CPUshare-.5) > 1e-9 || math.Abs(r.Sites[2].CPUshare-1.0/3) > 1e-9 {
		t.Fatal(r.Sites)
	}
	var total float64
	for _, s := range r.Sites {
		total += *s.Cost
	}
	if math.Abs(total-100) > 1e-9 {
		t.Fatal("allocation does not conserve budget", total)
	}
}
