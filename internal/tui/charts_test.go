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
	if len(rows) != 2 || rows[0] != "    ▄█ " || rows[1] != "_▄▄███ " {
		t.Fatalf("zero, small values, heights or gaps were misrepresented: %q", rows)
	}
	zero := 0.0
	rows = barPlot([]*float64{&zero, nil, &zero}, 2, 0)
	if rows[0] != "   " || rows[1] != "_ _" {
		t.Fatalf("a flat zero trend became missing data or a filled bar: %q", rows)
	}
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
					if !strings.ContainsRune(" █▄_", glyph) {
						t.Fatalf("chart uses a glyph unsupported by classic Windows fonts: %U", glyph)
					}
				}
			}
			// Detailed Host plots must not reintroduce the same fractional glyphs.
			plot := ansi.Strip(strings.Join(m.trendPanel("CPU history", "cpu_percent", cyan, 48), "\n"))
			if strings.ContainsAny(plot, "▁▂▃▅▆▇") {
				t.Fatal("Host history still uses unsupported fractional blocks")
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
