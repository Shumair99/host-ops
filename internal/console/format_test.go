package console

import (
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
func TestConsoleTextCannotInjectTerminalEscapes(t *testing.T) {
	s := Safe("hello\x1b[2J\x00world → done")
	for _, r := range s {
		if r > 127 || r == 27 || r == 0 {
			t.Fatal("unsafe terminal text")
		}
	}
}
