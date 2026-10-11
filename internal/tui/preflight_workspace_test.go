package tui

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestPreflightNamesEveryPropertyAndExplainsPassingEvidence(t *testing.T) {
	m := testDashboard(t)
	sites := []config.Site{{ID: "a", Proxy: config.Proxy{Domain: "a.example"}}, {ID: "b", Proxy: config.Proxy{Domain: "b.example"}}}
	checks := []model.Check{{Layer: "docker_boot", State: model.Pass, Summary: "Enabled at boot"}, {SiteID: "a", Layer: "site_health", State: model.Pass, Summary: "healthy"}, {SiteID: "b", Layer: "site_health", State: model.Fail, Summary: "DNS failed"}}
	text := ansi.Strip(strings.Join(m.preflightRows(checks, sites, 100), "\n"))
	for _, expected := range []string{"Host / shared infrastructure", "a.example", "b.example", "Enabled at boot", "DNS failed"} {
		if !strings.Contains(text, expected) {
			t.Fatal(text)
		}
	}
}
