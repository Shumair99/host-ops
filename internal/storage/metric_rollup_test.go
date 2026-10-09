package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
)

func seedMetric(t *testing.T, s *Store, host, site, kind string, at time.Time, values map[string]float64, unavailable ...string) {
	t.Helper()
	data, err := json.Marshal(metrics.Sample{HostID: host, SiteID: site, ObservedAt: at, Values: values, Unavailable: unavailable})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = insertObservation(context.Background(), tx, host, site, kind, at, data, 0); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func consolidate(t *testing.T, s *Store, policy config.History, now time.Time) {
	t.Helper()
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE history_sequence SET value=value WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = rollupMetrics(context.Background(), tx, policy.Defaults(), now); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestMetricTiersKeepPeaksKnownCountsCoverageAndIdentity(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	start := now.Add(-2 * 24 * time.Hour)
	for _, pair := range [][2]string{{"h", "s"}, {"h", "other"}, {"other-host", "s"}} {
		for i, n := range []float64{0, 90, 30} {
			at := start.Add([]time.Duration{0, 15 * time.Second, 45 * time.Second}[i])
			values := map[string]float64{"cpu_percent": n}
			missing := []string{}
			if i > 0 {
				values["memory_percent"] = float64(84 + i*4)
			} else {
				missing = append(missing, "memory unavailable")
			}
			seedMetric(t, s, pair[0], pair[1], "metrics", at, values, missing...)
		}
		seedMetric(t, s, pair[0], pair[1], "metrics", now, map[string]float64{"cpu_percent": 5})
	}
	consolidate(t, s, config.History{}, now)
	points, err := s.MetricHistory(ctx, "h", "s", "metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 1 {
		t.Fatal(points, err)
	}
	p := points[0]
	cpu, mem := p.Values["cpu_percent"], p.Values["memory_percent"]
	if p.Samples != 3 || cpu.Count != 3 || cpu.Min != 0 || cpu.Max != 90 || cpu.Mean != 40 || cpu.First != 0 || cpu.Last != 30 || mem.Count != 2 || mem.Mean != 90 || p.Unavailable["memory unavailable"] != 1 {
		t.Fatal("lost peaks/counts or fabricated zeros", p)
	}
	if fmt.Sprint(p.ObservedSeconds) != "[0 15 45]" || !mem.FirstAt.Equal(start.Add(15*time.Second)) || !mem.LastAt.Equal(start.Add(45*time.Second)) {
		t.Fatal("coverage/endpoints", p)
	}
	raw, err := s.Query(ctx, "h", "s", "metrics", time.Time{}, 100)
	if err != nil || len(raw) != 1 || !raw[0].ObservedAt.Equal(now) {
		t.Fatal("recent readings changed", raw, err)
	}
	// A six-day advance folds the minute windows into a five-minute window.
	consolidate(t, s, config.History{}, now.Add(6*24*time.Hour))
	points, err = s.MetricHistory(ctx, "h", "s", "metrics", time.Time{}, "30m", 100)
	if err != nil || len(points) != 1 || points[0].Samples != 3 || points[0].Values["cpu_percent"].Max != 90 || points[0].Values["memory_percent"].Count != 2 || fmt.Sprint(points[0].ObservedSeconds) != "[0 15 45]" {
		t.Fatal(points, err)
	}
	minutes, _ := s.Query(ctx, "h", "s", "metrics_5m", time.Time{}, 100)
	if len(minutes) != 0 {
		t.Fatal("minute source was not removed", minutes)
	}
	// Consolidation is idempotent, including changes in the global clock.
	consolidate(t, s, config.History{}, now.Add(6*24*time.Hour+time.Minute))
	again, _ := s.MetricHistory(ctx, "h", "s", "metrics", time.Time{}, "30m", 100)
	if len(again) != 1 || again[0].Samples != 3 {
		t.Fatal("recounted samples", again)
	}
}

func TestMetricProtectedTailIsNeverCountedTwice(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	start := now.Add(-2 * 24 * time.Hour)
	for i, n := range []float64{0, 90, 30} {
		seedMetric(t, s, "h", "", "host_metrics", start.Add(time.Duration(i)*15*time.Second), map[string]float64{"cpu_percent": n})
	}
	for i := 0; i < 2; i++ {
		consolidate(t, s, config.History{}, now)
		points, err := s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "auto", 100)
		if err != nil || len(points) != 1 || points[0].Samples != 3 || points[0].Values["cpu_percent"].Mean != 40 {
			t.Fatal(points, err)
		}
		raw, _ := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
		if len(raw) != 1 {
			t.Fatal(raw)
		}
	}
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 1})
	consolidate(t, s, config.History{}, now)
	points, err := s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 1 || points[0].Samples != 3 || points[0].Values["cpu_percent"].Mean != 40 {
		t.Fatal("tail was counted again", points, err)
	}
}

func TestMetricTiersFullModeBoundariesAndUnknownShapes(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	old := now.Add(-2 * 24 * time.Hour)
	seedMetric(t, s, "h", "", "host_metrics", old, map[string]float64{"cpu_percent": 10})
	seedMetric(t, s, "h", "", "host_metrics", now.Add(-24*time.Hour+time.Second), map[string]float64{"cpu_percent": 20})
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 30})
	consolidate(t, s, config.History{MetricDetail: "full"}, now)
	raw, _ := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
	if len(raw) != 3 {
		t.Fatal("full mode thinned readings", raw)
	}
	consolidate(t, s, config.History{}, now)
	raw, _ = s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
	if len(raw) != 2 {
		t.Fatal("24-hour boundary", raw)
	}
	if _, err := s.AddUnique(ctx, "h", "", "host_metrics", old, metrics.Sample{ObservedAt: old, Values: map[string]float64{"cpu_percent": 10}}); err == nil {
		t.Fatal("late import into a summarized window silently accepted")
	}
	// A custom record must never be destructively interpreted as a Sample.
	if _, err := s.Add(ctx, "h", "custom", "metrics", old, map[string]any{"values": map[string]float64{"cpu_percent": 9}, "new_field": "preserve me"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(ctx, "h", "custom", "metrics", now, map[string]int{"current": 1}); err != nil {
		t.Fatal(err)
	}
	consolidate(t, s, config.History{}, now)
	custom, _ := s.Query(ctx, "h", "custom", "metrics", time.Time{}, 100)
	if len(custom) != 2 {
		t.Fatal("unrecognized data was thinned", custom)
	}
}

func TestMetricSummaryFailureRollsBackRawRemoval(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	old := now.Add(-2 * 24 * time.Hour)
	seedMetric(t, s, "h", "", "host_metrics", old, map[string]float64{"cpu_percent": 9})
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 1})
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = insertObservation(ctx, tx, "h", "", "host_metrics_5m", old, []byte(`{"n":0}`), 0)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = rollupMetrics(ctx, tx, (config.History{}).Defaults(), now); err == nil {
		tx.Rollback()
		t.Fatal("invalid summary accepted")
	}
	tx.Rollback()
	raw, err := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
	if err != nil || len(raw) != 2 {
		t.Fatal("failed consolidation lost readings", raw, err)
	}
}

func TestMetricTupleSmallCountsAndWeightedMerging(t *testing.T) {
	for _, tuple := range []metricTuple{{1, 0, 0, 0, 0, 0, 0, 0}, {2, 45, 0, 90, 0, 90, 0, 15e9}, {2, 45, 0, 90, 90, 0, 0, 15e9}, {3, 40, 0, 90, 0, 30, 0, 45e9}} {
		data, err := json.Marshal(tuple)
		if err != nil {
			t.Fatal(err)
		}
		var decoded metricTuple
		if err = json.Unmarshal(data, &decoded); err != nil || decoded != tuple {
			t.Fatal(string(data), tuple, decoded, err)
		}
	}
	w := metricWindow{}
	at := time.Now().UTC().Truncate(time.Minute)
	for i, n := range []float64{math.MaxFloat64, -math.MaxFloat64, 0} {
		data, _ := json.Marshal(metrics.Sample{ObservedAt: at.Add(time.Duration(i) * 15 * time.Second), Values: map[string]float64{"x": n}})
		source, ok := rawMetric(observation{at: at.Add(time.Duration(i) * 15 * time.Second), data: data}, 60)
		if !ok {
			t.Fatal("finite input rejected")
		}
		if err := mergeWindow(&w, source, 0, 60); err != nil {
			t.Fatal(err)
		}
	}
	if w.V["x"][1] != 0 || w.V["x"][2] != -math.MaxFloat64 || w.V["x"][3] != math.MaxFloat64 {
		t.Fatal(w)
	}
}

func TestMetricSummaryPruneRepairsOutOfOrderActiveTail(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	makeData := func(at time.Time, value float64) []byte {
		data, _ := json.Marshal(metrics.Sample{ObservedAt: at, Values: map[string]float64{"cpu_percent": value}})
		w, ok := rawMetric(observation{at: at, data: data}, 1800)
		if !ok {
			t.Fatal("bad fixture")
		}
		data, _ = json.Marshal(w)
		return data
	}
	add := func(at time.Time, value float64) {
		tx, err := s.DB.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = insertObservation(ctx, tx, "h", "", "host_metrics_30m", at, makeData(at, value), 0); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	add(now.Add(-3*24*time.Hour), 10)
	add(now.Add(-40*24*time.Hour), 99)
	if err := s.Prune(ctx, config.History{}); err != nil {
		t.Fatal(err)
	}
	add(now.Add(-2*24*time.Hour), 20)
	points, err := s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "30m", 100)
	if err != nil || len(points) != 2 || points[0].Values["cpu_percent"].Mean != 20 || points[1].Values["cpu_percent"].Mean != 10 {
		t.Fatal(points, err)
	}
}

func TestMetricAutomaticMaintenanceAndPartialAgeBoundary(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	old := now.Add(-2 * 24 * time.Hour)
	seedMetric(t, s, "h", "", "host_metrics", old, map[string]float64{"cpu_percent": 7})
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 1})
	_, err := s.Add(ctx, "h", "", "host_metrics", now.Add(time.Second), metrics.Sample{ObservedAt: now.Add(time.Second), Values: map[string]float64{"cpu_percent": 2}})
	if err != nil {
		t.Fatal(err)
	}
	points, err := s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 1 || points[0].Values["cpu_percent"].Max != 7 {
		t.Fatal("writer did not automatically consolidate history", points, err)
	}
	boundary := time.Now().UTC().Add(-7 * 24 * time.Hour).Truncate(30 * time.Minute)
	data, _ := json.Marshal(metrics.Sample{ObservedAt: boundary, Values: map[string]float64{"cpu_percent": 8}})
	w, ok := rawMetric(observation{at: boundary, data: data}, 300)
	if !ok {
		t.Fatal("bad fixture")
	}
	data, _ = json.Marshal(w)
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = insertObservation(ctx, tx, "h", "", "host_metrics_5m", boundary, data, 0); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.Prune(ctx, config.History{}); err != nil {
		t.Fatal(err)
	}
	points, err = s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 2 {
		t.Fatal("partial age boundary lost data", points, err)
	}
}

func TestMetricCompressedPartialBlockReopensAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	old := now.Add(-2 * 24 * time.Hour)
	seedMetric(t, s, "h", "", "host_metrics", old, map[string]float64{"cpu_percent": 90})
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 1})
	consolidate(t, s, config.History{}, now)
	var compressed int
	if err = s.DB.QueryRow(`SELECT count(*) FROM blocks WHERE encoding=2`).Scan(&compressed); err != nil || compressed != 1 {
		s.Close()
		t.Fatal(compressed, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	consolidate(t, s, config.History{}, now.Add(24*time.Hour+5*time.Minute))
	points, err := s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 1 || points[0].Samples != 1 || points[0].Values["cpu_percent"].Max != 90 {
		t.Fatal(points, err)
	}
	// Append a second older window to the reopened compressed partial block.
	seedMetric(t, s, "h", "", "host_metrics", now.Add(time.Minute), map[string]float64{"cpu_percent": 2})
	seedMetric(t, s, "h", "", "host_metrics", now.Add(24*time.Hour+5*time.Minute), map[string]float64{"cpu_percent": 3})
	consolidate(t, s, config.History{}, now.Add(24*time.Hour+5*time.Minute))
	points, err = s.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "5m", 100)
	if err != nil || len(points) != 2 || points[0].Samples != 2 || points[0].Values["cpu_percent"].Mean != 1.5 || points[1].Values["cpu_percent"].Max != 90 {
		t.Fatal(points, err)
	}
}

func TestVersionFourMigrationKeepsExistingFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ctx := context.Background()
	seedMetric(t, s, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 7})
	before, err := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	_, err = s.DB.Exec(`DROP INDEX blocks_active; CREATE UNIQUE INDEX blocks_active ON blocks(stream_id) WHERE encoding=0; PRAGMA user_version=4`)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 100)
	if err != nil || len(after) != 1 || after[0].ID != before[0].ID || !after[0].ObservedAt.Equal(before[0].ObservedAt) || string(after[0].Data) != string(before[0].Data) {
		t.Fatal(before, after, err)
	}
}

func TestMetricHistorySnapshotAcrossConcurrentConsolidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	reader, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(30 * time.Minute)
	seedMetric(t, reader, "h", "", "host_metrics", now.Add(-2*24*time.Hour), map[string]float64{"cpu_percent": 90})
	seedMetric(t, reader, "h", "", "host_metrics", now, map[string]float64{"cpu_percent": 1})
	tx, err := reader.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	raw, err := queryBlocksTx(ctx, tx, "h", "", "host_metrics", time.Time{}, 100, false, false, true)
	if err != nil || len(raw) != 2 {
		t.Fatal(raw, err)
	}
	consolidate(t, writer, config.History{}, now)
	summaries, err := queryBlocksTx(ctx, tx, "h", "", "host_metrics_5m", time.Time{}, 100, false, false, true)
	if err != nil || len(summaries) != 0 {
		t.Fatal("read mixed raw and consolidated snapshots", summaries, err)
	}
	tx.Rollback()
	points, err := reader.MetricHistory(ctx, "h", "", "host_metrics", time.Time{}, "auto", 100)
	if err != nil || len(points) != 2 || points[0].Samples+points[1].Samples != 2 {
		t.Fatal(points, err)
	}
}

func TestMetricHistoryLimitUsesMeasuredTimeAfterLateArrival(t *testing.T) {
	s := auditStore(t)
	now := time.Now().UTC()
	for _, pair := range []struct {
		at    time.Time
		value float64
	}{{now.Add(-2 * time.Minute), 1}, {now.Add(-time.Minute), 2}, {now.Add(-time.Hour), 3}} {
		seedMetric(t, s, "h", "", "host_metrics", pair.at, map[string]float64{"cpu_percent": pair.value})
	}
	points, err := s.MetricHistory(context.Background(), "h", "", "host_metrics", time.Time{}, "raw", 1)
	if err != nil || len(points) != 1 || points[0].Values["cpu_percent"].Mean != 2 {
		t.Fatal(points, err)
	}
}

func FuzzMetricWindowDecoder(f *testing.F) {
	coverage := make([]byte, 38)
	coverage[0] = 1
	seed, _ := json.Marshal(metricWindow{N: 1, C: coverage, V: map[string]metricTuple{"cpu_percent": {1, 0, 0, 0, 0, 0, 0, 0}}})
	f.Add(seed)
	f.Add([]byte(`{"n":-1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			t.Skip()
		}
		for _, seconds := range []int{300, 1800} {
			w, err := decodeWindow(observation{data: data}, seconds)
			if err == nil {
				if err = mergeWindow(&metricWindow{}, w, 0, seconds); err != nil {
					t.Fatal("accepted window could not be merged", err)
				}
			}
		}
	})
}
