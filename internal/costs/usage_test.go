package costs

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestIdleAndFullCapacityDoNotBecomeRelativeSiteShares(t *testing.T) {
	for _, full := range []bool{false, true} {
		c, samples, hosts := lowUsageFixture()
		site := c.Sites[0]
		site.ID, site.Runtime.Container = "b", "b"
		c.Sites = append(c.Sites, site)
		cpu, ram, hostCPU, hostRAM := 0.0, 0.0, 0.0, 0.0
		if full {
			cpu, ram, hostCPU, hostRAM = 200, 500, 100, 1000
		}
		for _, id := range []string{"a", "b"} {
			samples[id] = []metrics.Sample{{ObservedAt: hosts[0].ObservedAt, Values: map[string]float64{"cpu_percent": cpu, "memory_used_bytes": ram}}}
		}
		hosts[0].Values["cpu_percent"], hosts[0].Values["memory_used_bytes"] = hostCPU, hostRAM
		r := Allocate(c, samples, hosts)
		want := 0.0
		if full {
			want = 100
		}
		closeTo(t, r.SiteCost, want)
		closeTo(t, r.UnusedCost, 100-want)
		closeTo(t, r.OtherCost, 0)
	}
}

func TestUnknownWorkloadDoesNotReceiveOrRedistributeBill(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	c.Sites = append(c.Sites, config.Site{ID: "missing", Runtime: config.Runtime{Type: "docker", Container: "missing"}, Business: config.Business{MonthlyFee: 20}})
	r := Allocate(c, samples, hosts)
	closeTo(t, r.Sites[0].Cost, 15)
	if r.Sites[1].Cost != nil || r.Sites[1].CPUshare != nil || r.Sites[1].Margin != nil || r.MeasuredSites != 1 {
		t.Fatal("missing workload received a fabricated resource share", r.Sites)
	}
	closeTo(t, r.SiteCost, 15)
	closeTo(t, r.UnallocatedCost, 85)
}

func TestMissingStaleAndInvalidMeasurementsRemainUnknown(t *testing.T) {
	cases := map[string]func(map[string][]metrics.Sample, *[]metrics.Sample){
		"missing host":         func(_ map[string][]metrics.Sample, h *[]metrics.Sample) { *h = nil },
		"missing cores":        func(_ map[string][]metrics.Sample, h *[]metrics.Sample) { delete((*h)[0].Values, "cpu_count") },
		"missing RAM capacity": func(_ map[string][]metrics.Sample, h *[]metrics.Sample) { delete((*h)[0].Values, "memory_total_bytes") },
		"stale host": func(_ map[string][]metrics.Sample, h *[]metrics.Sample) {
			(*h)[0].ObservedAt = time.Now().Add(-4 * time.Minute)
		},
		"stale workload": func(s map[string][]metrics.Sample, _ *[]metrics.Sample) {
			s["a"][0].ObservedAt = time.Now().Add(-4 * time.Minute)
		},
		"future host": func(_ map[string][]metrics.Sample, h *[]metrics.Sample) {
			(*h)[0].ObservedAt = time.Now().Add(5 * time.Minute)
		},
		"NaN CPU": func(s map[string][]metrics.Sample, _ *[]metrics.Sample) { s["a"][0].Values["cpu_percent"] = math.NaN() },
		"infinite RAM": func(s map[string][]metrics.Sample, _ *[]metrics.Sample) {
			s["a"][0].Values["memory_used_bytes"] = math.Inf(1)
		},
		"negative CPU":   func(s map[string][]metrics.Sample, _ *[]metrics.Sample) { s["a"][0].Values["cpu_percent"] = -1 },
		"impossible RAM": func(s map[string][]metrics.Sample, _ *[]metrics.Sample) { s["a"][0].Values["memory_used_bytes"] = 1001 },
		"missing latest counter": func(s map[string][]metrics.Sample, _ *[]metrics.Sample) {
			s["a"] = append(s["a"], metrics.Sample{ObservedAt: time.Now().Add(time.Second)})
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, samples, hosts := lowUsageFixture()
			mutate(samples, &hosts)
			r := Allocate(c, samples, hosts)
			if r.Sites[0].Cost != nil || r.Sites[0].CapacityUsage != nil {
				t.Fatal("unusable measurements priced as observed usage", r.Sites[0])
			}
			if _, err := json.Marshal(r); err != nil {
				t.Fatal("invalid numbers escaped into cost output", err)
			}
		})
	}
}

func TestCapacityIsMatchedToTheTimeOfEachWorkloadSample(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	old := hosts[0]
	old.ObservedAt = old.ObservedAt.Add(-30 * time.Second)
	old.Values = map[string]float64{"cpu_count": 2, "cpu_percent": 60, "memory_total_bytes": 1000, "memory_used_bytes": 600}
	hosts[0].Values["memory_total_bytes"] = 2000
	hosts = append(hosts, old) // Input ordering cannot alter matching.
	samples["a"] = []metrics.Sample{
		{ObservedAt: old.ObservedAt, Values: map[string]float64{"cpu_percent": 100, "memory_used_bytes": 200}},
		{ObservedAt: hosts[0].ObservedAt, Values: map[string]float64{"cpu_percent": 100, "memory_used_bytes": 200}},
		{ObservedAt: old.ObservedAt.Add(-time.Hour), Values: map[string]float64{"cpu_percent": 400, "memory_used_bytes": 1000}},
	}
	r := Allocate(c, samples, hosts)
	closeTo(t, r.Sites[0].CPUshare, .375)
	closeTo(t, r.Sites[0].MemoryShare, .15)
	closeTo(t, r.Sites[0].Cost, 26.25)
	if r.Sites[0].Samples != 2 {
		t.Fatal("unmatched old usage entered current estimate", r.Sites[0])
	}
}

func TestSharedContainersAreCountedOnceButSeparateWWWIsIndependent(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	c.Sites[0].Proxy.Domain = "example.com"
	alias := c.Sites[0]
	alias.ID, alias.Proxy.Domain = "alias", "alias.example.com"
	www := c.Sites[0]
	www.ID, www.Proxy.Domain, www.Runtime.Container = "www", "www.example.com", "different-app"
	c.Sites = append(c.Sites, alias, www)
	samples[alias.ID], samples[www.ID] = samples["a"], samples["a"]
	r := Allocate(c, samples, hosts)
	if len(r.Sites) != 2 {
		t.Fatal("domain names were used instead of actual runtime ownership", r.Sites)
	}
	closeTo(t, r.SiteCost, 30)
	for _, a := range r.Sites {
		closeTo(t, a.Cost, 15)
	}
	if len(r.Sites[0].SiteIDs) != 2 {
		t.Fatal("shared workload lost its address links", r.Sites)
	}
}

func TestLegacyEqualWeightCannotForceIdleSitesToPay(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	c.Host.CostWeights = config.CostWeights{Equal: .4, Memory: .3, CPU: .3}
	r := Allocate(c, samples, hosts)
	closeTo(t, r.SiteCost, 15)
	if r.Weights.Equal != 0 || r.Weights.CPU != .5 || r.Weights.Memory != .5 || c.Host.CostWeights.Equal != .4 {
		t.Fatal("legacy weight did not adapt without mutating config", r.Weights)
	}
	c.Host.CostWeights = config.CostWeights{CPU: 1}
	delete(samples["a"][0].Values, "memory_used_bytes")
	r = Allocate(c, samples, hosts)
	closeTo(t, r.SiteCost, 10)
}

func TestImpossibleCombinedUsageIsNotRescaledIntoFullBill(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	samples["a"][0].Values["memory_used_bytes"] = 800
	b := c.Sites[0]
	b.ID, b.Runtime.Container = "b", "b"
	c.Sites = append(c.Sites, b)
	samples["b"] = samples["a"]
	r := Allocate(c, samples, hosts)
	if r.SiteCost != nil || r.Sites[0].Cost != nil || r.Sites[1].Cost != nil || r.OtherCost != nil {
		t.Fatal("conflicting ownership/counters were silently normalized", r)
	}
}

func TestAssessLoadsHostDenominatorsAndUsesBillingBudget(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, samples, hosts := lowUsageFixture()
	budget := 100.0
	c.Host.Billing = &config.Billing{Provider: "manual", MonthlyAmount: &budget}
	for _, h := range hosts {
		if _, err = db.Add(ctx, "h", "", "host_metrics", h.ObservedAt, h); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range samples["a"] {
		if _, err = db.Add(ctx, "h", "a", "metrics", s.ObservedAt, s); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Assess(ctx, c, db)
	if err != nil {
		t.Fatal(err)
	}
	closeTo(t, r.SiteCost, 15)
	closeTo(t, r.UnusedCost, 75)
	if r.Billing == nil || r.Billing.Provider != "manual" || r.HostSamples != 1 {
		t.Fatal("billing/host evidence was lost", r)
	}
}
