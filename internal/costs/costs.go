package costs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/billing"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Allocation struct {
	HostID         string   `json:"host_id"`
	SiteID         string   `json:"site_id"`
	SiteIDs        []string `json:"site_ids,omitempty"`
	Name           string   `json:"name"`
	Samples        int      `json:"samples"`
	CPUshare       *float64 `json:"cpu_share"` // Fraction of total host CPU capacity, not of other sites' usage.
	MemoryShare    *float64 `json:"memory_share"`
	CapacityUsage  *float64 `json:"capacity_usage"`
	Cost           *float64 `json:"estimated_monthly_cost"`
	Revenue        *float64 `json:"monthly_revenue"`
	Margin         *float64 `json:"estimated_margin"`
	NoisyNeighbour bool     `json:"noisy_neighbour"`
}
type Report struct {
	Billing         *billing.Report    `json:"billing,omitempty"`
	HostID          string             `json:"host_id"`
	ObservedAt      time.Time          `json:"observed_at"`
	WindowStart     time.Time          `json:"window_start"`
	WindowEnd       time.Time          `json:"window_end"`
	HostSamples     int                `json:"host_samples"`
	Currency        string             `json:"currency"`
	TotalCost       *float64           `json:"total_monthly_cost"`
	SiteCost        *float64           `json:"estimated_site_monthly_cost"`
	UnallocatedCost *float64           `json:"unallocated_monthly_cost"`
	UnusedCost      *float64           `json:"estimated_unused_monthly_cost"`
	OtherCost       *float64           `json:"estimated_other_monthly_cost"`
	HostCPUUsage    *float64           `json:"host_cpu_usage"`
	HostMemoryUsage *float64           `json:"host_memory_usage"`
	CapacityUsage   *float64           `json:"capacity_usage"`
	MeasuredSites   int                `json:"measured_sites"`
	Weights         config.CostWeights `json:"weights"`
	Formula         string             `json:"formula"`
	Sites           []Allocation       `json:"sites"`
	Notes           []string           `json:"notes"`
}

// Allocate requires measured host capacity as well as workload observations.
// Missing denominators must never become equal shares of the monthly bill.
func Allocate(c config.Config, samples map[string][]metrics.Sample, hostSamples []metrics.Sample) Report {
	var total *float64
	if c.Host.Billing != nil && c.Host.Billing.MonthlyAmount != nil {
		total = number(*c.Host.Billing.MonthlyAmount + c.Host.MonthlyBackupCost)
	} else if c.Host.Billing == nil && c.Host.MonthlyCost+c.Host.MonthlyBackupCost > 0 {
		total = number(c.Host.MonthlyCost + c.Host.MonthlyBackupCost)
	}
	return allocate(c, samples, hostSamples, total)
}

type allocationGroup struct {
	Site config.Site
	IDs  []string
}

func runtimeIdentity(s config.Site) string {
	switch s.Runtime.Type {
	case "docker", "podman":
		if s.Runtime.Container != "" {
			return s.HostID + "/" + s.Runtime.Type + "/" + s.Runtime.Container
		}
	case "systemd":
		if s.Runtime.Unit != "" {
			return s.HostID + "/systemd/" + s.Runtime.Unit
		}
	}
	return ""
}

// Combine confirmed shared runtimes/redirects only. A www address with a
// different application remains independent; domain spelling is not ownership.
func allocationGroups(sites []config.Site) []allocationGroup {
	ordered := config.OrderedSites(sites)
	byDomain := map[string]config.Site{}
	domain := func(s string) string { return strings.ToLower(strings.TrimSuffix(s, ".")) }
	for _, s := range ordered {
		byDomain[domain(s.Proxy.Domain)] = s
	}
	out := []allocationGroup{}
	indices := map[string]int{}
	for _, s := range ordered {
		identity := runtimeIdentity(s)
		if identity == "" && s.Proxy.RedirectTo != "" {
			if target, ok := byDomain[domain(s.Proxy.RedirectTo)]; ok && target.ID != s.ID {
				identity = runtimeIdentity(target)
				if identity == "" {
					identity = "site/" + target.ID
				}
				if _, exists := indices[identity]; !exists {
					indices[identity] = len(out)
					out = append(out, allocationGroup{Site: target})
				}
			}
		}
		if identity == "" {
			identity = "site/" + s.ID
		}
		if i, exists := indices[identity]; exists {
			out[i].IDs = append(out[i].IDs, s.ID)
			if s.Proxy.Domain != "" && strings.HasSuffix(domain(out[i].Site.Proxy.Domain), "."+domain(s.Proxy.Domain)) {
				out[i].Site = s
			}
		} else {
			indices[identity] = len(out)
			out = append(out, allocationGroup{Site: s, IDs: []string{s.ID}})
		}
	}
	return out
}

const pairingAge = 90 * time.Second

func number(n float64) *float64 { return &n }
func usable(n float64) bool     { return n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }
func weighted(cpu, memory *float64, w config.CostWeights) *float64 {
	if w.CPU > 0 && cpu == nil || w.Memory > 0 && memory == nil {
		return nil
	}
	n := 0.0
	if cpu != nil {
		n += w.CPU * *cpu
	}
	if memory != nil {
		n += w.Memory * *memory
	}
	return number(n)
}
func cleanSamples(samples []metrics.Sample, host string, now time.Time) []metrics.Sample {
	out := []metrics.Sample{}
	seen := map[int64]bool{}
	for _, s := range samples {
		if s.ObservedAt.IsZero() || s.ObservedAt.Before(now.Add(-24*time.Hour)) || s.ObservedAt.After(now.Add(time.Minute)) || s.HostID != "" && s.HostID != host {
			continue
		}
		key := s.ObservedAt.UnixNano()
		if !seen[key] {
			seen[key] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObservedAt.Before(out[j].ObservedAt) })
	return out
}
func nearestHost(hosts []metrics.Sample, at time.Time) *metrics.Sample {
	i := sort.Search(len(hosts), func(i int) bool { return !hosts[i].ObservedAt.Before(at) })
	best := -1
	for _, candidate := range []int{i - 1, i} {
		if candidate < 0 || candidate >= len(hosts) {
			continue
		}
		if best < 0 || hosts[candidate].ObservedAt.Sub(at).Abs() < hosts[best].ObservedAt.Sub(at).Abs() {
			best = candidate
		}
	}
	if best < 0 || hosts[best].ObservedAt.Sub(at).Abs() > pairingAge {
		return nil
	}
	return &hosts[best]
}
func fraction(values map[string]float64, key string, denominator float64) *float64 {
	n, exists := values[key]
	if !exists || !usable(n) || !usable(denominator) || denominator <= 0 || n > denominator {
		return nil
	}
	return number(n / denominator)
}

type average struct {
	sum   float64
	count int
}

func (a *average) add(n *float64) {
	if n != nil {
		a.sum += *n
		a.count++
	}
}
func (a average) value() *float64 {
	if a.count == 0 {
		return nil
	}
	return number(a.sum / float64(a.count))
}

func allocate(c config.Config, samples map[string][]metrics.Sample, hostSamples []metrics.Sample, total *float64) Report {
	w := c.Host.CostWeights
	legacyEqual := w.Equal
	if w.Equal+w.Memory+w.CPU == 0 {
		w = config.CostWeights{Memory: .5, CPU: .5}
	} else {
		// The old equal component no longer reserves part of the entire bill.
		w.CPU += w.Equal / 2
		w.Memory += w.Equal / 2
		w.Equal = 0
	}
	r := Report{HostID: c.Host.ID, ObservedAt: time.Now().UTC(), Currency: c.Host.Currency, TotalCost: total, Weights: w,
		Formula: "monthly budget * (cpu_weight * workload_CPU / total_host_CPU + memory_weight * workload_RAM / total_host_RAM)",
		Sites:   []Allocation{}, Notes: []string{
			"Usage values capacity consumed; the hosting bill still covers the whole server. Unused capacity is not a billing discount.",
			"CPU is measured across all host cores; memory uses host RAM, not the container's memory limit. Disk, traffic and backup usage are not measured in this estimate.",
			"Recent matched observations are projected to a month, not a monthly usage history. Missing measurements remain unknown.",
		}}
	if legacyEqual > 0 {
		r.Notes = append(r.Notes, "Legacy equal weight is split equally between CPU and RAM; no fixed part of the bill is distributed among sites.")
	}
	hosts := cleanSamples(hostSamples, c.Host.ID, r.ObservedAt)
	if len(hosts) > 0 && model.Fresh(hosts[len(hosts)-1].ObservedAt, r.ObservedAt, model.DefaultMaxAge) {
		r.WindowStart, r.WindowEnd = hosts[0].ObservedAt, hosts[len(hosts)-1].ObservedAt
		r.HostSamples = len(hosts)
		cpu, memory := average{}, average{}
		for _, s := range hosts {
			cpu.add(fraction(s.Values, "cpu_percent", 100))
			memory.add(fraction(s.Values, "memory_used_bytes", s.Values["memory_total_bytes"]))
		}
		latest := hosts[len(hosts)-1]
		// Don't conceal a current missing counter behind older good readings.
		if fraction(latest.Values, "cpu_percent", 100) != nil {
			r.HostCPUUsage = cpu.value()
		}
		if fraction(latest.Values, "memory_used_bytes", latest.Values["memory_total_bytes"]) != nil {
			r.HostMemoryUsage = memory.value()
		}
		r.CapacityUsage = weighted(r.HostCPUUsage, r.HostMemoryUsage, w)
	} else {
		hosts = nil
		r.Notes = append(r.Notes, "Fresh host measurements unavailable; resource usage cannot be priced without measured server capacity.")
	}
	if total == nil {
		r.Notes = append(r.Notes, "Host cost is not configured; estimates and margins remain unknown.")
	}
	ownedSites := append([]config.Site(nil), c.Sites...)
	for i := range ownedSites {
		if ownedSites[i].HostID == "" {
			ownedSites[i].HostID = c.Host.ID
		}
	}
	groups := allocationGroups(ownedSites)
	revenueTotal := 0.0
	for _, g := range groups {
		revenueTotal += g.Site.Business.MonthlyFee
	}
	for _, g := range groups {
		// Aliases contain copies of the same readings, not extra resource usage.
		var readings []metrics.Sample
		for _, id := range g.IDs {
			candidate := cleanSamples(samples[id], c.Host.ID, r.ObservedAt)
			if len(candidate) > 0 && (len(readings) == 0 || candidate[len(candidate)-1].ObservedAt.After(readings[len(readings)-1].ObservedAt) || candidate[len(candidate)-1].ObservedAt.Equal(readings[len(readings)-1].ObservedAt) && len(candidate) > len(readings)) {
				readings = candidate
			}
		}
		a := Allocation{HostID: c.Host.ID, SiteID: g.Site.ID, SiteIDs: g.IDs, Name: g.Site.Proxy.Domain}
		if a.Name == "" {
			a.Name = g.Site.Name
		}
		if a.Name == "" {
			a.Name = g.Site.ID
		}
		cpu, memory := average{}, average{}
		var currentCPU, currentMemory *float64
		for i, s := range readings {
			if len(hosts) == 0 || s.ObservedAt.Before(r.WindowStart.Add(-pairingAge)) || s.ObservedAt.After(r.WindowEnd.Add(pairingAge)) {
				continue
			}
			h := nearestHost(hosts, s.ObservedAt)
			if h == nil {
				continue
			}
			var cpuFraction *float64
			if g.Site.Runtime.Type == "docker" || g.Site.Runtime.Type == "podman" {
				cores := h.Values["cpu_count"]
				if cores >= 1 && math.Trunc(cores) == cores {
					cpuFraction = fraction(s.Values, "cpu_percent", cores*100)
				}
			}
			memFraction := fraction(s.Values, "memory_used_bytes", h.Values["memory_total_bytes"])
			cpu.add(cpuFraction)
			memory.add(memFraction)
			if cpuFraction != nil || memFraction != nil {
				a.Samples++
			}
			if i == len(readings)-1 && model.Fresh(s.ObservedAt, r.ObservedAt, model.DefaultMaxAge) {
				currentCPU, currentMemory = cpuFraction, memFraction
			}
		}
		if currentCPU != nil {
			a.CPUshare = cpu.value()
		}
		if currentMemory != nil {
			a.MemoryShare = memory.value()
		}
		a.CapacityUsage = weighted(a.CPUshare, a.MemoryShare, w)
		if total != nil && a.CapacityUsage != nil {
			a.Cost = number(*total * *a.CapacityUsage)
		}
		if total != nil && *total == 0 {
			a.Cost = number(0)
		}
		if g.Site.Business.MonthlyFee > 0 {
			a.Revenue = number(g.Site.Business.MonthlyFee)
		}
		if a.Revenue != nil && a.MemoryShare != nil && revenueTotal > 0 {
			a.NoisyNeighbour = *a.MemoryShare > .3 && *a.MemoryShare > 2**a.Revenue/revenueTotal
		}
		r.Sites = append(r.Sites, a)
	}
	// Conflicting ownership/counters cannot justify more than the whole host.
	// Keep this unknown instead of rescaling into a full monthly bill.
	cpuSum, memorySum := 0.0, 0.0
	for _, s := range r.Sites {
		if s.CPUshare != nil {
			cpuSum += *s.CPUshare
		}
		if s.MemoryShare != nil {
			memorySum += *s.MemoryShare
		}
	}
	if w.CPU > 0 && cpuSum > 1.000001 || w.Memory > 0 && memorySum > 1.000001 {
		for i := range r.Sites {
			r.Sites[i].Cost, r.Sites[i].CapacityUsage = nil, nil
		}
		r.Notes = append(r.Notes, "Workload totals exceed measured server capacity; site costs remain unknown until conflicting ownership or measurements are resolved.")
	}
	subtotal := 0.0
	for i := range r.Sites {
		s := &r.Sites[i]
		if total != nil && *total == 0 {
			s.Cost = number(0)
		}
		if s.CapacityUsage != nil {
			r.MeasuredSites++
		}
		if s.Cost != nil {
			subtotal += *s.Cost
			if s.Revenue != nil {
				s.Margin = number(*s.Revenue - *s.Cost)
			}
		}
	}
	if total != nil {
		if *total == 0 {
			r.UnusedCost, r.OtherCost = number(0), number(0)
		}
		if r.MeasuredSites > 0 || *total == 0 || len(r.Sites) == 0 {
			r.SiteCost = number(subtotal)
		}
		if r.SiteCost != nil {
			r.UnallocatedCost = number(math.Max(0, *total-subtotal))
		}
		if r.CapacityUsage != nil {
			r.UnusedCost = number(*total * (1 - *r.CapacityUsage))
			if r.SiteCost != nil {
				other := *total**r.CapacityUsage - subtotal
				if other >= -0.000001 {
					r.OtherCost = number(math.Max(0, other))
				} else {
					r.Notes = append(r.Notes, "Site and host observations disagree on used capacity; shared usage is unknown rather than forcing the estimates to balance.")
				}
			}
		}
	}
	if r.MeasuredSites < len(r.Sites) {
		r.Notes = append(r.Notes, fmt.Sprintf("%d of %d workloads have enough matched CPU/RAM evidence for a usage estimate. Shared and unmeasured usage is not redistributed to measured sites.", r.MeasuredSites, len(r.Sites)))
	}
	return r
}

func Assess(ctx context.Context, c config.Config, store *storage.Store) (Report, error) {
	observation, err := billing.Resolve(ctx, c.Host, store, false)
	if err != nil {
		return Report{HostID: c.Host.ID, ObservedAt: time.Now().UTC(), Notes: []string{"Billing unavailable: " + err.Error(), "Open Costs / setup to connect a billing source."}}, nil
	}
	c.Sites = append([]config.Site(nil), c.Sites...)
	notes := []string{"Confirmed shared runtimes and redirects are counted once; independent subdomains keep their own usage estimates."}
	if observation.Currency != c.Host.Currency {
		for i := range c.Sites {
			c.Sites[i].Business.MonthlyFee = 0
		}
		notes = append(notes, "Hosting fees need the same configured currency as provider pricing; no currency conversion is assumed.")
	}
	c.Host.Currency = observation.Currency
	read := func(site, kind string) ([]metrics.Sample, error) {
		records, err := store.Query(ctx, c.Host.ID, site, kind, time.Now().Add(-24*time.Hour), 1000)
		if err != nil {
			return nil, err
		}
		out := []metrics.Sample{}
		for _, record := range records {
			var sample metrics.Sample
			if json.Unmarshal(record.Data, &sample) == nil {
				out = append(out, sample)
			}
		}
		return out, nil
	}
	hostSamples, err := read("", "host_metrics")
	if err != nil {
		return Report{}, err
	}
	samples := map[string][]metrics.Sample{}
	for _, site := range c.Sites {
		if samples[site.ID], err = read(site.ID, "metrics"); err != nil {
			return Report{}, err
		}
	}
	r := allocate(c, samples, hostSamples, observation.MonthlyEstimate)
	r.Billing = &observation
	r.Notes = append(r.Notes, notes...)
	r.Notes = append(r.Notes, observation.Notes...)
	return r, nil
}
