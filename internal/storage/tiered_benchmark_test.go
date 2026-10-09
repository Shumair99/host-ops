package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
)

// One identical day of resource measurements at each age. This is a measured
// aging comparison, not a multiplication of fixed overhead into a daily rate.
func TestTieredMetricStorageComparison(t *testing.T) {
	if os.Getenv("HOSTOPS_STORAGE_BENCH") != "1" {
		t.Skip("set HOSTOPS_STORAGE_BENCH=1 for the tiered size fixture")
	}
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	start := now.Add(-3 * 24 * time.Hour)
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	write := func(site, kind string, sample metrics.Sample) {
		data, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = insertObservation(ctx, tx, "h", site, kind, sample.ObservedAt, data, 0); err != nil {
			t.Fatal(err)
		}
		count++
	}
	for cycle := 0; cycle < 2880; cycle++ {
		at := start.Add(time.Duration(cycle)*30*time.Second + time.Duration((cycle*7919)%1_000_000_000)*time.Nanosecond)
		for i := 0; i < 21; i++ {
			memory := 100000000 + float64((cycle*7919+i*31)%5000)*4096
			site := fmt.Sprintf("site-%d", i)
			write(site, "metrics", metrics.Sample{HostID: "h", SiteID: site, ObservedAt: at, Values: map[string]float64{"cpu_percent": 5 + 4*math.Sin(float64(cycle+i)*.17), "memory_used_bytes": memory, "memory_limit_bytes": 512 << 20, "memory_percent": 100 * memory / (512 << 20), "pids": float64(5 + (cycle+i)%4)}})
		}
		for _, offset := range []time.Duration{0, 15 * time.Second} {
			cpu := 10 + 9*math.Sin(float64(cycle)*.31)
			memory := 38 + math.Sin(float64(cycle)*.01)
			write("", "host_metrics", metrics.Sample{HostID: "h", ObservedAt: at.Add(offset), Values: map[string]float64{"cpu_percent": cpu, "memory_percent": memory, "memory_total_bytes": 4 << 30, "memory_used_bytes": memory * (4 << 30) / 100, "swap_used_bytes": 918 << 20, "uptime_seconds": float64(cycle)*30 + offset.Seconds(), "load_1": cpu / 100, "load_5": cpu / 150, "load_15": cpu / 200, "disk_total_bytes": 40 << 30, "disk_used_bytes": float64(35<<30) + float64(cycle)*4096, "disk_free_bytes": float64(5<<30) - float64(cycle)*4096, "disk_percent": 87.5 + float64(cycle)/100000, "disk_reserved_bytes": 0, "network_rx_bytes": float64(cycle) * 150000, "network_tx_bytes": float64(cycle) * 170000, "network_rx_bytes_per_second": vRate(cycle, 12000), "network_tx_bytes_per_second": vRate(cycle, 15000)}})
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	size := func() int64 {
		var n int64
		if err = s.DB.QueryRow(`SELECT page_count*page_size FROM pragma_page_count(),pragma_page_size()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	rawSize := size()
	t.Logf("%d changing resource readings for one day across 21 sites: raw %.3f MiB", count, float64(rawSize)/(1<<20))
	for _, phase := range []struct {
		advance time.Duration
		kind    string
	}{{0, "5m"}, {6 * 24 * time.Hour, "30m"}} {
		begin := time.Now()
		consolidate(t, s, config.History{}, now.Add(phase.advance))
		physical := size()
		var covered int
		for i := -1; i < 21; i++ {
			site, kind := "", "host_metrics"
			if i >= 0 {
				site = fmt.Sprintf("site-%d", i)
				kind = "metrics"
			}
			records, err := s.queryBlocks(ctx, "h", site, kind+"_"+phase.kind, time.Time{}, 10000, true, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range records {
				_, seconds, _ := summaryKind(r.Kind)
				w, err := decodeWindow(observation{data: r.Data}, seconds)
				if err != nil {
					t.Fatal(err)
				}
				covered += w.N
			}
			raw, err := s.Query(ctx, "h", site, kind, time.Time{}, 1000)
			if err != nil {
				t.Fatal(err)
			}
			covered += len(raw)
		}
		if covered != count {
			t.Fatal("lost or duplicated source readings", phase.kind, covered, count)
		}
		t.Logf("%s: %.3f MiB, %.2f%% smaller than raw; consolidation %s; all %d readings represented", phase.kind, float64(physical)/(1<<20), 100*(1-float64(physical)/float64(rawSize)), time.Since(begin), covered)
	}
}
