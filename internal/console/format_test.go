package console

import (
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/metrics"
	"strings"
	"testing"
)

func TestUnknownAndIncompleteMetricsAreExplicit(t *testing.T) {
	s := strings.Join(Format("disk", metrics.DiskReport{Directories: []metrics.DiskEntry{{Path: "/srv", Bytes: 1024, Partial: true}}, Unavailable: []string{"permission denied"}}), "\n")
	if !strings.Contains(s, ">= 1.0 KiB") || !strings.Contains(s, "UNKNOWN") {
		t.Fatal(s)
	}
	if !strings.Contains(Bar(nil, "cpu_percent", "CPU"), "UNKNOWN") {
		t.Fatal("missing CPU displayed as zero")
	}
}

func TestCostsShowsSetupInsteadOfUnknownTableAndHidesUnconfiguredFees(t *testing.T) {
	text := strings.Join(Format("costs", costs.Report{}), "\n")
	if strings.Contains(text, "ALLOCATED COST") || !strings.Contains(text, "billing set") {
		t.Fatal("unconfigured cost view still shows an empty allocation table", text)
	}
	amount := 10.0
	text = strings.Join(Format("costs", costs.Report{TotalCost: &amount, Currency: "GBP", Sites: []costs.Allocation{{Name: "site", Cost: &amount}}}), "\n")
	if strings.Contains(text, "HOSTING FEE") || !strings.Contains(text, "GBP 10.00") || !strings.Contains(text, "Host capacity used: CPU UNKNOWN") {
		t.Fatal("unconfigured client fees still dominate cost view", text)
	}
}
func TestConsoleTextCannotInjectTerminalEscapes(t *testing.T) {
	s := Safe("hello\x1b[2J\x00world → done")
	for _, r := range s {
		if r > 127 || r == 27 || r == 0 {
			t.Fatal("unsafe terminal text")
		}
	}
}

func TestFormatJSONMetricsGolden(t *testing.T) {
	got := strings.Join(FormatJSON("metrics", []byte(`{"host_id":"node-1","values":{"cpu_percent":25}}`)), "\n")
	want := strings.Join([]string{
		"HOST node-1  |  Observed unobserved",
		"CPU    [#####---------------]  25.0%",
		"RAM    [????????????????????] UNKNOWN",
		"DISK   [????????????????????] UNKNOWN",
	}, "\n")
	if got != want {
		t.Fatalf("metric output changed:\n got: %q\nwant: %q", got, want)
	}
	if got := FormatJSON("metrics", []byte("{")); len(got) != 1 || got[0] != "Evidence could not be rendered." {
		t.Fatalf("invalid metric JSON was not handled safely: %q", got)
	}
}
