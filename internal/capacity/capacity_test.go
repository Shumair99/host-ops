package capacity

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"testing"
	"time"
)

func TestSparseHistoryDoesNotInventForecast(t *testing.T) {
	now := time.Now()
	r := Evaluate("h", []metrics.Sample{{ObservedAt: now, Values: map[string]float64{"disk_percent": 88, "disk_free_bytes": 4e9}}}, now)
	if r.State != model.Warn || r.CPU95 != nil || r.DaysTo85 != nil || !r.AlreadyAbove85 {
		t.Fatal(r)
	}
}

func TestForecastUsesOldestObservationBeyondRecentQueryLimit(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Now()
	old := metrics.Sample{ObservedAt: now.Add(-48 * time.Hour), Values: map[string]float64{"cpu_percent": 99, "disk_used_bytes": 1000, "disk_free_bytes": 9000, "disk_percent": 10}}
	if _, err = db.Add(ctx, "h", "", "host_metrics", old.ObservedAt, old); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		sample := metrics.Sample{ObservedAt: now.Add(time.Duration(i-1000) * 15 * time.Second), Values: map[string]float64{"cpu_percent": 10, "memory_percent": 20, "disk_used_bytes": 2000, "disk_free_bytes": 8000, "disk_percent": 20}}
		if _, err = db.Add(ctx, "h", "", "host_metrics", sample.ObservedAt, sample); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Assess(ctx, config.Config{Host: config.Host{ID: "h"}}, db)
	if err != nil || r.DiskGrowthBytesPerDay == nil || r.DaysTo85 == nil || r.CPU95 == nil || *r.CPU95 != 10 || r.Samples != 1000 {
		t.Fatal("oldest disk observation was lost or contaminated recent percentiles", err, r)
	}
}

func TestEmptyOrStaleSamplesCannotPass(t *testing.T) {
	now := time.Now()
	for _, s := range []metrics.Sample{{ObservedAt: now, Values: map[string]float64{}}, {ObservedAt: now.Add(-time.Hour), Values: map[string]float64{"disk_percent": 20}}} {
		if Evaluate("h", []metrics.Sample{s}, now).State != model.Unknown {
			t.Fatal("missing evidence passed")
		}
	}
}
