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
			for i, n := range values {
				m.snapshot.History = append(m.snapshot.History, metrics.Sample{
					ObservedAt: time.Unix(int64(i*15), 0),
					Values:     map[string]float64{"cpu_percent": n},
				})
			}
			spark := m.spark("cpu_percent", len(values), cyan)
			if len(spark) != 2 {
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
