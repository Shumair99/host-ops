package costs

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"math"
	"testing"
)

func TestAllocationConservesCostAndUnknownInputs(t *testing.T) {
	c := config.Config{Host: config.Host{MonthlyCost: 10}, Sites: []config.Site{{ID: "a"}, {ID: "b"}}}
	r := Allocate(c, map[string][]metrics.Sample{"a": {{Values: map[string]float64{"memory_used_bytes": 75, "cpu_percent": 10}}}, "b": {{Values: map[string]float64{"memory_used_bytes": 25, "cpu_percent": 0}}}})
	if math.Abs(*r.Sites[0].Cost+*r.Sites[1].Cost-10) > .000001 {
		t.Fatal("cost not conserved", r)
	}
	c.Host.MonthlyCost = 0
	r = Allocate(c, nil)
	if r.TotalCost != nil || r.Sites[0].Cost != nil || r.Sites[0].Margin != nil {
		t.Fatal("missing costs fabricated")
	}
}
