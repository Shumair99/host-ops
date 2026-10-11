package storage

import (
	"context"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func TestLatestMetricsKeepsQuietPropertyWhenAnotherHasLargeHistory(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	at := time.Now()
	add := func(host, site string, i int) {
		t.Helper()
		sample := metrics.Sample{HostID: host, SiteID: site, ObservedAt: at.Add(time.Duration(i) * time.Second), Values: map[string]float64{"cpu_percent": float64(i)}}
		if _, err := s.Add(ctx, host, site, "metrics", sample.ObservedAt, sample); err != nil {
			t.Fatal(err)
		}
	}
	add("h", "quiet", 0)
	for i := 1; i <= 260; i++ {
		add("h", "busy", i)
	}
	add("other", "ignored", 1)
	rows, err := s.LatestMetrics(ctx, "h")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].SiteID != "busy" || rows[1].SiteID != "quiet" || !rows[0].ObservedAt.Equal(at.Add(260*time.Second)) {
		t.Fatal("latest readings were starved or crossed hosts", rows)
	}
}
