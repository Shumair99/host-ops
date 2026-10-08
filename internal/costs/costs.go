package costs

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/billing"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"time"
)

type Allocation struct {
	HostID         string   `json:"host_id"`
	SiteID         string   `json:"site_id"`
	Name           string   `json:"name"`
	Samples        int      `json:"samples"`
	CPUshare       float64  `json:"cpu_share"`
	MemoryShare    float64  `json:"memory_share"`
	Cost           *float64 `json:"estimated_monthly_cost"`
	Revenue        *float64 `json:"monthly_revenue"`
	Margin         *float64 `json:"estimated_margin"`
	NoisyNeighbour bool     `json:"noisy_neighbour"`
}
type Report struct {
	Billing    *billing.Report    `json:"billing,omitempty"`
	HostID     string             `json:"host_id"`
	ObservedAt time.Time          `json:"observed_at"`
	Currency   string             `json:"currency"`
	TotalCost  *float64           `json:"total_monthly_cost"`
	Weights    config.CostWeights `json:"weights"`
	Formula    string             `json:"formula"`
	Sites      []Allocation       `json:"sites"`
	Notes      []string           `json:"notes"`
}

func Allocate(c config.Config, samples map[string][]metrics.Sample) Report {
	var total *float64
	if c.Host.Billing != nil && c.Host.Billing.MonthlyAmount != nil {
		amount := *c.Host.Billing.MonthlyAmount + c.Host.MonthlyBackupCost
		total = &amount
	} else if c.Host.Billing == nil && c.Host.MonthlyCost+c.Host.MonthlyBackupCost > 0 {
		amount := c.Host.MonthlyCost + c.Host.MonthlyBackupCost
		total = &amount
	}
	return allocate(c, samples, total)
}

// Allocations follow displayed properties rather than charging an observed
// base domain and its www variant twice. Other domains remain independent.
func allocationSites(sites []config.Site) []config.Site {
	byDomain := map[string]bool{}
	for _, site := range sites {
		byDomain[strings.ToLower(strings.TrimSuffix(site.Proxy.Domain, "."))] = true
	}
	out := []config.Site{}
	for _, site := range sites {
		domain := strings.ToLower(strings.TrimSuffix(site.Proxy.Domain, "."))
		if strings.HasPrefix(domain, "www.") && byDomain[strings.TrimPrefix(domain, "www.")] {
			continue
		}
		out = append(out, site)
	}
	return out
}

func allocate(c config.Config, samples map[string][]metrics.Sample, total *float64) Report {
	c.Sites = allocationSites(c.Sites)
	w := c.Host.CostWeights
	if w.Equal+w.Memory+w.CPU == 0 {
		w = config.CostWeights{Equal: .4, Memory: .3, CPU: .3}
	}
	r := Report{HostID: c.Host.ID, ObservedAt: time.Now().UTC(), Currency: c.Host.Currency, Weights: w, Formula: "monthly estimate * (equal_weight / property_count + memory_weight * memory_share + cpu_weight * cpu_share)", Sites: []Allocation{}, Notes: []string{"Allocation is a budget estimate across monitored properties, not financial accounting.", "Incomplete or zero aggregate usage assigns equal shares to that resource's allocation component.", "Each property's window contains at most the latest 1,000 records in 24 hours; unmanaged services are shared overhead."}}
	if total != nil {
		r.TotalCost = total
	} else {
		r.Notes = append(r.Notes, "Host cost is not configured; estimates and margins remain unknown.")
	}
	cpus, memories := map[string]float64{}, map[string]float64{}
	cpuTotal, memoryTotal, revenueTotal := 0.0, 0.0, 0.0
	cpuComplete, memoryComplete := true, true
	for _, site := range c.Sites {
		cpuCount, memoryCount := 0, 0
		for _, sample := range samples[site.ID] {
			if value, ok := sample.Values["cpu_percent"]; ok {
				cpus[site.ID] += value
				cpuCount++
			}
			if value, ok := sample.Values["memory_used_bytes"]; ok {
				memories[site.ID] += value
				memoryCount++
			}
		}
		if cpuCount > 0 {
			cpus[site.ID] /= float64(cpuCount)
		}
		if memoryCount > 0 {
			memories[site.ID] /= float64(memoryCount)
		}
		cpuComplete = cpuComplete && cpuCount > 0
		memoryComplete = memoryComplete && memoryCount > 0
		cpuTotal += cpus[site.ID]
		memoryTotal += memories[site.ID]
		revenueTotal += site.Business.MonthlyFee
	}
	for _, site := range c.Sites {
		equal := 1 / float64(len(c.Sites))
		cpuShare, memoryShare := equal, equal
		if cpuTotal > 0 && cpuComplete {
			cpuShare = cpus[site.ID] / cpuTotal
		}
		if memoryTotal > 0 && memoryComplete {
			memoryShare = memories[site.ID] / memoryTotal
		}
		a := Allocation{HostID: site.HostID, SiteID: site.ID, Name: site.Name, Samples: len(samples[site.ID]), CPUshare: cpuShare, MemoryShare: memoryShare}
		if r.TotalCost != nil {
			cost := *r.TotalCost * (w.Equal*equal + w.Memory*memoryShare + w.CPU*cpuShare)
			a.Cost = &cost
		}
		if site.Business.MonthlyFee > 0 {
			fee := site.Business.MonthlyFee
			a.Revenue = &fee
			if a.Cost != nil {
				margin := fee - *a.Cost
				a.Margin = &margin
			}
			if revenueTotal > 0 && memoryTotal > 0 && memoryComplete {
				a.NoisyNeighbour = memoryShare > .3 && memoryShare > 2*fee/revenueTotal
			}
		}
		r.Sites = append(r.Sites, a)
	}
	return r
}
func Assess(ctx context.Context, c config.Config, store *storage.Store) (Report, error) {
	observation, err := billing.Resolve(ctx, c.Host, store, false)
	if err != nil {
		return Report{HostID: c.Host.ID, ObservedAt: time.Now().UTC(), Notes: []string{"Billing unavailable: " + err.Error(), "Use hostops billing list / set / sync to connect an explicit billing source."}}, nil
	}
	c.Sites = allocationSites(c.Sites)
	notes := []string{"Base/www domain pairs are allocated once; other domains sharing a backend are not unique resource allocations."}
	if observation.Currency != c.Host.Currency {
		for i := range c.Sites {
			c.Sites[i].Business.MonthlyFee = 0
		}
		notes = append(notes, "Hosting fees need the same configured currency as provider pricing; no currency conversion is assumed.")
	}
	c.Host.Currency = observation.Currency
	samples := map[string][]metrics.Sample{}
	for _, site := range c.Sites {
		records, err := store.Query(ctx, c.Host.ID, site.ID, "metrics", time.Now().Add(-24*time.Hour), 1000)
		if err != nil {
			return Report{}, err
		}
		for _, record := range records {
			var sample metrics.Sample
			if json.Unmarshal(record.Data, &sample) == nil {
				samples[site.ID] = append(samples[site.ID], sample)
			}
		}
	}
	r := allocate(c, samples, observation.MonthlyEstimate)
	r.Billing = &observation
	r.Notes = append(r.Notes, notes...)
	r.Notes = append(r.Notes, observation.Notes...)
	return r, nil
}
