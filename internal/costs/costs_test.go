package costs

import (
	"context"
	"github.com/Shumair99/host-ops/internal/billing"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
	"math"
	"testing"
	"time"
)

func TestAllocationPricesActualCapacityAndKeepsUnusedBudget(t *testing.T) {
	c, samples, hosts := lowUsageFixture()
	r := Allocate(c, samples, hosts)
	closeTo(t, r.TotalCost, 100)
	closeTo(t, r.Sites[0].CPUshare, .1)
	closeTo(t, r.Sites[0].MemoryShare, .2)
	closeTo(t, r.Sites[0].Cost, 15)
	closeTo(t, r.SiteCost, 15)
	closeTo(t, r.UnallocatedCost, 85)
	closeTo(t, r.UnusedCost, 75)
	closeTo(t, r.OtherCost, 10)
	closeTo(t, number(*r.SiteCost+*r.OtherCost+*r.UnusedCost), 100)
	c.Host.MonthlyCost = 0
	r = Allocate(c, samples, hosts)
	if r.TotalCost != nil || r.Sites[0].Cost != nil || r.Sites[0].Margin != nil {
		t.Fatal("missing costs fabricated")
	}
}

func lowUsageFixture() (config.Config, map[string][]metrics.Sample, []metrics.Sample) {
	now := time.Now().UTC()
	c := config.Config{Host: config.Host{ID: "h", MonthlyCost: 100, Currency: "GBP", CostWeights: config.CostWeights{CPU: .5, Memory: .5}}, Sites: []config.Site{{ID: "a", HostID: "h", Runtime: config.Runtime{Type: "docker", Container: "a"}}}}
	samples := map[string][]metrics.Sample{"a": {{HostID: "h", ObservedAt: now, Values: map[string]float64{"cpu_percent": 40, "memory_used_bytes": 200, "memory_percent": 99}}}}
	hosts := []metrics.Sample{{HostID: "h", ObservedAt: now, Values: map[string]float64{"cpu_count": 4, "cpu_percent": 20, "memory_total_bytes": 1000, "memory_used_bytes": 300}}}
	return c, samples, hosts
}

func closeTo(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("got %v, want %.9f", got, want)
	}
}

func TestProviderCurrencyCannotSilentlyRepriceClientFees(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	amount := 12.0
	observation := billing.Report{Provider: "hetzner", ResourceID: "42", Currency: "EUR", ObservedAt: now, MonthlyEstimate: &amount}
	if err = db.Transition(ctx, "h", "", "billing", observation, "", now, nil); err != nil {
		t.Fatal(err)
	}
	c := config.Config{Host: config.Host{ID: "h", Currency: "GBP", Billing: &config.Billing{Provider: "hetzner", ResourceID: "42"}}, Sites: []config.Site{{ID: "s", Business: config.Business{MonthlyFee: 20}}}}
	r, err := Assess(ctx, c, db)
	if err != nil || r.Currency != "EUR" || r.Sites[0].Revenue != nil || r.Sites[0].Margin != nil || c.Sites[0].Business.MonthlyFee != 20 {
		t.Fatal("different-currency fees were converted or configuration was mutated", err, r)
	}
}

func TestWWWIsNotChargedTwiceAndExplicitZeroBudgetIsKnown(t *testing.T) {
	zero := 0.0
	c := config.Config{Host: config.Host{Billing: &config.Billing{Provider: "manual", MonthlyAmount: &zero}}, Sites: []config.Site{{ID: "base", Proxy: config.Proxy{Domain: "example.com"}}, {ID: "www", Proxy: config.Proxy{Domain: "www.example.com", RedirectTo: "example.com"}}, {ID: "other", Proxy: config.Proxy{Domain: "other.example.com"}}}}
	r := Allocate(c, nil, nil)
	if len(r.Sites) != 2 || r.TotalCost == nil || *r.TotalCost != 0 || r.Sites[0].Cost == nil || *r.Sites[0].Cost != 0 {
		t.Fatal("aliases or known zero were handled incorrectly", r)
	}
}
