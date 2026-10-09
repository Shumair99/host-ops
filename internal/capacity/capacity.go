package capacity

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"sort"
	"time"
)

type Report struct {
	HostID                string      `json:"host_id"`
	ObservedAt            time.Time   `json:"observed_at"`
	State                 model.State `json:"state"`
	Samples               int         `json:"samples"`
	CPU95                 *float64    `json:"cpu_p95"`
	Memory95              *float64    `json:"memory_p95"`
	DiskPercent           *float64    `json:"disk_percent"`
	FreeBytes             *float64    `json:"disk_free_bytes"`
	DiskGrowthBytesPerDay *float64    `json:"disk_growth_bytes_per_day"`
	DaysTo85              *float64    `json:"days_to_85_percent"`
	AlreadyAbove85        bool        `json:"already_above_85_percent"`
	Notes                 []string    `json:"notes"`
}

func p95(values []float64) *float64 {
	if len(values) < 20 {
		return nil
	}
	sort.Float64s(values)
	index := (95*len(values)+99)/100 - 1
	value := values[index]
	return &value
}
func Evaluate(host string, samples []metrics.Sample, now time.Time) Report {
	r := Report{HostID: host, ObservedAt: now.UTC(), State: model.Unknown, Samples: len(samples), Notes: []string{"Percentiles need at least 20 samples; forecasts need at least 24 hours of disk observations.", "A linear disk projection is a scenario based on observed growth, not a guarantee.", "No additional-site count is inferred without representative workload measurements."}}
	if len(samples) == 0 {
		return r
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].ObservedAt.Before(samples[j].ObservedAt) })
	latest := samples[len(samples)-1]
	cpu, mem := []float64{}, []float64{}
	for _, s := range samples {
		if n, ok := s.Values["cpu_percent"]; ok {
			cpu = append(cpu, n)
		}
		if n, ok := s.Values["memory_percent"]; ok {
			mem = append(mem, n)
		}
	}
	r.CPU95 = p95(cpu)
	r.Memory95 = p95(mem)
	if value, ok := latest.Values["disk_percent"]; ok {
		r.DiskPercent = &value
		r.AlreadyAbove85 = value >= 85
	}
	if value, ok := latest.Values["disk_free_bytes"]; ok {
		r.FreeBytes = &value
	}
	if model.Fresh(latest.ObservedAt, now, model.DefaultMaxAge) && r.DiskPercent != nil && latest.Values != nil {
		r.State = model.Pass
		if r.DiskPercent != nil && *r.DiskPercent >= 80 || r.Memory95 != nil && *r.Memory95 >= 80 || r.CPU95 != nil && *r.CPU95 >= 80 {
			r.State = model.Warn
		}
		if r.DiskPercent != nil && *r.DiskPercent >= 95 || r.Memory95 != nil && *r.Memory95 >= 95 {
			r.State = model.Fail
		}
	} else {
		r.Notes = append(r.Notes, "The latest host sample is stale or lacks disk measurements.")
	}
	first := samples[0]
	days := latest.ObservedAt.Sub(first.ObservedAt).Hours() / 24
	firstUsed, firstOK := first.Values["disk_used_bytes"]
	lastUsed, lastOK := latest.Values["disk_used_bytes"]
	if days >= 1 && firstOK && lastOK {
		growth := (lastUsed - firstUsed) / days
		r.DiskGrowthBytesPerDay = &growth
		if growth > 0 && r.FreeBytes != nil && !r.AlreadyAbove85 {
			remaining := .85*(lastUsed+*r.FreeBytes) - lastUsed
			forecast := remaining / growth
			r.DaysTo85 = &forecast
		}
	}
	return r
}
func Assess(ctx context.Context, c config.Config, store *storage.Store) (Report, error) {
	now := time.Now()
	since := now.Add(-30 * 24 * time.Hour)
	records, err := store.Query(ctx, c.Host.ID, "", "host_metrics", since, 1000)
	if err != nil {
		return Report{}, err
	}
	samples := []metrics.Sample{}
	for _, record := range records {
		var s metrics.Sample
		if json.Unmarshal(record.Data, &s) == nil {
			samples = append(samples, s)
		}
	}
	r := Evaluate(c.Host.ID, samples, now)
	r.Notes = append(r.Notes, "CPU/RAM percentiles use the latest 1,000 individual readings; averages in older summaries are excluded. Disk growth compares measured endpoints within retained history up to 30 days.")
	first, err := store.OldestMetricPoint(ctx, c.Host.ID, "host_metrics", since)
	if err != nil {
		return Report{}, err
	}
	if first != nil && len(samples) > 0 {
		if used, ok := first.Values["disk_used_bytes"]; ok && !used.FirstAt.Before(since) {
			earliest := metrics.Sample{HostID: c.Host.ID, ObservedAt: used.FirstAt, Values: map[string]float64{"disk_used_bytes": used.First}}
			growth := Evaluate(c.Host.ID, []metrics.Sample{earliest, samples[len(samples)-1]}, now)
			r.DiskGrowthBytesPerDay, r.DaysTo85 = growth.DiskGrowthBytesPerDay, growth.DaysTo85
			if first.ResolutionSeconds > 0 {
				r.Notes = append(r.Notes, "The older disk endpoint is an actual reading preserved in a summary, not the window average.")
			}
		}
	}
	return r, nil
}
