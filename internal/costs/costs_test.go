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
	c := config.Config{Host: config.Host{Billing: &config.Billing{Provider: "manual", MonthlyAmount: &zero}}, Sites: []config.Site{{ID: "base", Proxy: config.Proxy{Domain: "example.com"}}, {ID: "www", Proxy: config.Proxy{Domain: "www.example.com"}}, {ID: "other", Proxy: config.Proxy{Domain: "other.example.com"}}}}
	r := Allocate(c, nil)
	if len(r.Sites) != 2 || r.TotalCost == nil || *r.TotalCost != 0 || r.Sites[0].Cost == nil || *r.Sites[0].Cost != 0 {
		t.Fatal("aliases or known zero were handled incorrectly", r)
	}
}
