package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/costs"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/charmbracelet/x/ansi"
)

func TestCostWorkspaceShowsUsageAndSpareCapacityInsteadOfFullBillShares(t *testing.T) {
	m := testDashboard(t)
	c := config.Config{Host: config.Host{ID: "cost-test", MonthlyCost: 100, Currency: "GBP"}, Sites: []config.Site{{ID: "store", HostID: "cost-test", Runtime: config.Runtime{Type: "docker", Container: "store"}, Proxy: config.Proxy{Domain: "store.example.invalid"}}}}
	at := time.Now().UTC()
	site := metrics.Sample{HostID: c.Host.ID, ObservedAt: at, Values: map[string]float64{"cpu_percent": 40, "memory_used_bytes": 200}}
	host := metrics.Sample{HostID: c.Host.ID, ObservedAt: at, Values: map[string]float64{"cpu_count": 4, "cpu_percent": 20, "memory_total_bytes": 1000, "memory_used_bytes": 300}}
	m.snapshot.Sites, m.source.Config = c.Sites, c
	m.snapshot.Detail = costs.Allocate(c, map[string][]metrics.Sample{"store": {site}}, []metrics.Sample{host})
	for _, width := range []int{60, 100, 140} {
		lines := m.detailLines(width)
		text := ansi.Strip(strings.Join(lines, "\n"))
		for _, want := range []string{"GBP 100.00", "GBP 15.00", "GBP 75.00", "GBP 10.00", "25.0% used", "15.0% of host capacity", "1/1 workloads", "Unused capacity", "Sites / measured usage"} {
			if !strings.Contains(text, want) {
				t.Fatalf("width %d omitted %q: %s", width, want, text)
			}
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d overflows: %s", width, line)
			}
		}
	}
	if dir := os.Getenv("HOSTOPS_PREVIEW_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		m.page, m.width, m.height, m.menuFocus, m.theme.color = "costs", 120, 40, false, true
		if err := os.WriteFile(filepath.Join(dir, "demo-costs-usage.txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
		m.width, m.height, m.offset = 60, 16, 8
		if err := os.WriteFile(filepath.Join(dir, "demo-costs-usage-narrow.txt"), []byte(m.frame()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.snapshot.Detail = costs.Allocate(c, nil, []metrics.Sample{host})
	text := ansi.Strip(strings.Join(m.detailLines(100), "\n"))
	if !strings.Contains(text, "0/1 workloads") || !strings.Contains(text, "Usage measurements unavailable") || !strings.Contains(text, "GBP 75.00") {
		t.Fatal("unknown usage was priced or spare capacity was concealed", text)
	}
}
