package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/charmbracelet/x/ansi"
)

func TestBarPlotPreservesZeroSmallValuesAndMissingSamples(t *testing.T) {
	values := []float64{0, 0.1, 25, 50, 75, 100}
	points := make([]*float64, len(values)+1)
	for i := range values {
		points[i] = &values[i]
	}
	rows := barPlot(points, 2, 100)
	if len(rows) != 2 || ansi.Strip(rows[0]) != "    ▄█ " || ansi.Strip(rows[1]) != " ▄▄███ " || strings.Count(rows[1], "\x1b[4m") != 1 || strings.Count(rows[1], "\x1b[24m") != 1 {
		t.Fatalf("zero, small values, heights or gaps were misrepresented: %q", rows)
	}
	zero := 0.0
	rows = barPlot([]*float64{&zero, nil, &zero}, 2, 0)
	if ansi.Strip(rows[0]) != "   " || ansi.Strip(rows[1]) != "   " || strings.Count(rows[1], "\x1b[4m") != 2 || strings.Count(rows[1], "\x1b[24m") != 2 {
		t.Fatalf("a flat zero trend became missing data or a filled bar: %q", rows)
	}
	rows = barPlot([]*float64{&zero, &zero, &zero}, 2, 0)
	if ansi.Strip(rows[1]) != "   " || strings.Count(rows[1], "\x1b[4m") != 3 || strings.Count(rows[1], "\x1b[24m") != 3 {
		t.Fatalf("adjacent measured zeroes should form a continuous baseline, got %q", rows[1])
	}
}

func TestOverviewBucketsStayFixedUntilNextColumn(t *testing.T) {
	const width = 10
	bucketWidth := overviewTrendWindow / width
	base := time.Unix(1_700_000_000, 0)
	index := base.UnixNano() / int64(bucketWidth)
	boundary := time.Unix(0, (index+1)*int64(bucketWidth))
	old := metrics.Sample{
		ObservedAt: boundary.Add(-time.Second),
		Values:     map[string]float64{"cpu_percent": 42},
	}
	beforeBoundary := boundary.Add(-500 * time.Millisecond)
	steady := recentBuckets([]metrics.Sample{old}, "cpu_percent", width, beforeBoundary)
	unchanged := recentBuckets([]metrics.Sample{old}, "cpu_percent", width, beforeBoundary.Add(200*time.Millisecond))
	if !sameBuckets(steady, unchanged) {
		t.Fatal("redraw moved a point before its time bucket advanced", steady, unchanged)
	}
	if steady[width-1] == nil || *steady[width-1] != 42 {
		t.Fatal("current bucket lost its latest sample", steady)
	}

	newSample := metrics.Sample{
		ObservedAt: boundary.Add(100 * time.Millisecond),
		Values:     map[string]float64{"cpu_percent": 55},
	}
	advanced := recentBuckets([]metrics.Sample{old, newSample}, "cpu_percent", width, boundary.Add(200*time.Millisecond))
	if advanced[width-2] == nil || *advanced[width-2] != 42 || advanced[width-1] == nil || *advanced[width-1] != 55 {
		t.Fatal("new bucket did not advance the old point by one column", advanced)
	}
}

func TestOverviewKeepsExactThirtyMinuteWindow(t *testing.T) {
	width := 10
	now := time.Unix(1_700_000_000, 0).Add(17 * time.Second)
	inside := metrics.Sample{
		ObservedAt: now.Add(-overviewTrendWindow + time.Nanosecond),
		Values:     map[string]float64{"cpu_percent": 33},
	}
	points := recentBuckets([]metrics.Sample{inside}, "cpu_percent", width, now)
	if points[0] == nil || *points[0] != 33 {
		t.Fatal("oldest sample still inside the window was dropped", points)
	}
	expired := inside
	expired.ObservedAt = now.Add(-overviewTrendWindow - time.Nanosecond)
	points = recentBuckets([]metrics.Sample{expired}, "cpu_percent", width, now)
	for _, point := range points {
		if point != nil {
			t.Fatal("sample older than 30 minutes stayed visible", points)
		}
	}
}

func TestOverviewTrendLabelFitsAvailableWidth(t *testing.T) {
	for _, test := range []struct {
		width int
		want  string
	}{
		{width: 21, want: "Last 30 min · 0–100%"},
		{width: 20, want: "30m · 0–100%"},
		{width: 13, want: "30m · 0–100%"},
		{width: 12, want: "30m"},
	} {
		if got := overviewTrendLabel(test.width); got != test.want {
			t.Errorf("overviewTrendLabel(%d) = %q, want %q", test.width, got, test.want)
		}
	}
}

func TestOverviewMeasuredZeroUsesConnectedThinBaseline(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.History = []metrics.Sample{{ObservedAt: time.Now(), Values: map[string]float64{"cpu_percent": 0}}}
	rows := m.spark("cpu_percent", 3, cyan)
	row := rows[len(rows)-1]
	if got := ansi.Strip(row); got != "   " || strings.Count(row, "\x1b[4m") != 1 || strings.Count(row, "\x1b[24m") != 1 {
		t.Fatalf("zero baseline should underline one bottom-aligned cell, got %q", row)
	}
}

func sameBuckets(a, b []*float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i] == nil) != (b[i] == nil) {
			return false
		}
		if a[i] != nil && *a[i] != *b[i] {
			return false
		}
	}
	return true
}

func TestAllTrendLevelsUseClassicWindowsConsoleGlyphs(t *testing.T) {
	m := testDashboard(t)
	m.width = 120
	for _, colored := range []bool{false, true} {
		m.theme.color = colored
		for _, values := range [][]float64{{0, 0, 0}, {0, 0.001, 1, 2, 3, 4, 5, 6, 7, 8}, {38.8, 38.8, 38.8}} {
			m.snapshot.History = nil
			start := time.Now().Add(-time.Duration(len(values)-1) * 15 * time.Second)
			for i, n := range values {
				m.snapshot.History = append(m.snapshot.History, metrics.Sample{
					ObservedAt: start.Add(time.Duration(i*15) * time.Second),
					Values:     map[string]float64{"cpu_percent": n},
				})
			}
			spark := m.spark("cpu_percent", len(values), cyan)
			if len(spark) != 4 {
				t.Fatalf("sparkline has %d rows", len(spark))
			}
			for _, row := range spark {
				if ansi.StringWidth(row) != len(values) {
					t.Fatalf("chart width changed: %q", row)
				}
				for _, glyph := range ansi.Strip(row) {
					if !strings.ContainsRune(" █▄", glyph) {
						t.Fatalf("chart uses a glyph unsupported by classic Windows fonts: %U", glyph)
					}
				}
			}
			// Host plots use terminal underlines for zero, but not intermediate
			// fractional heights that can render inconsistently in classic consoles.
			plot := ansi.Strip(strings.Join(m.trendPanel("CPU history", "cpu_percent", cyan, 48), "\n"))
			if strings.ContainsAny(plot, "▂▃▅▆▇") {
				t.Fatal("Host history uses an unsupported intermediate fractional block")
			}
		}
	}
}

func TestOverviewExcludesOldAndFutureSamples(t *testing.T) {
	now := time.Now()
	samples := []metrics.Sample{{ObservedAt: now.Add(-time.Hour), Values: map[string]float64{"cpu_percent": 99}}, {ObservedAt: now.Add(time.Minute), Values: map[string]float64{"cpu_percent": 100}}, {ObservedAt: now.Add(-time.Second), Values: map[string]float64{"cpu_percent": 38}}}
	buckets := recentBuckets(samples, "cpu_percent", 12, now)
	for i, b := range buckets {
		if i < 11 && b != nil {
			t.Fatal("old/future sample shown", i, *b)
		}
	}
	if buckets[11] == nil || *buckets[11] != 38 {
		t.Fatal("recent sample lost", buckets)
	}
}
