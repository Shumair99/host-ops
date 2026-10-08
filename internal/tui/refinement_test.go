package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/x/ansi"
)

func TestOverviewNamesDoNotDisplaceUsefulChecksAndHiddenFailuresStayVisible(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.Sites[0].Name = "very-long-production-property-name.example.com"
	for i := range m.snapshot.Reports[0].Checks {
		if m.snapshot.Reports[0].Checks[i].Layer == "tls" {
			m.snapshot.Reports[0].Checks[i].State = model.Fail
		}
	}
	text := ansi.Strip(strings.Join(m.overviewSiteTable(95), "\n"))
	if strings.Contains(text, m.snapshot.Sites[0].Name) || !strings.Contains(text, "TLS") || !strings.Contains(text, "CRITICAL") || !strings.Contains(text, "Public HTTP") || !strings.Contains(text, "Runtime") {
		t.Fatal("long name displaced important evidence", text)
	}
	text = ansi.Strip(strings.Join(m.overviewSiteTable(38), "\n"))
	if !strings.Contains(text, "Attention: TLS") {
		t.Fatal("narrow overview hid a failed TLS check", text)
	}
}

func TestSitesWorkspaceKeepsLastSelectedPropertyAndMetricsVisible(t *testing.T) {
	m := testDashboard(t)
	base := m.snapshot.Sites[0]
	m.snapshot.Sites = nil
	for i := 0; i < 40; i++ {
		s := base
		s.ID = fmt.Sprintf("s%02d", i)
		s.Name = fmt.Sprintf("property%02d", i)
		s.Proxy.Domain = s.Name + ".example.invalid"
		m.snapshot.Sites = append(m.snapshot.Sites, s)
	}
	m.selected = 39
	m.page = "sites"
	m.menuFocus = false
	for _, size := range [][2]int{{80, 24}, {120, 36}} {
		m.width, m.height = size[0], size[1]
		frame := ansi.Strip(m.frame())
		if !strings.Contains(frame, "property39") || !strings.Contains(frame, "Selected property") || !strings.Contains(frame, "CPU") || !strings.Contains(frame, "Memory") || strings.Contains(frame, "Production properties") {
			t.Fatal("Sites repeats overview or loses selected data", frame)
		}
	}
}

func TestHeaderShowsFreshnessOnlyWhenHostTelemetryIsStale(t *testing.T) {
	m := testDashboard(t)
	m.source.Demo = false
	m.source.Live = true
	m.snapshot.Connection.Connected = true
	m.snapshot.Host.ObservedAt = time.Now()
	if strings.Contains(m.frame(), "Observed") || strings.Contains(m.frame(), "TELEMETRY STALE") {
		t.Fatal("fresh telemetry still animates a global age")
	}
	m.snapshot.Host.ObservedAt = time.Now().Add(-time.Minute)
	if !strings.Contains(m.frame(), "TELEMETRY STALE") {
		t.Fatal("stale connected telemetry was hidden")
	}
}

func TestRecentActivityResolvesLegacyIDsAndKeepsFutureRemovedNames(t *testing.T) {
	m := testDashboard(t)
	m.snapshot.Sites[0].ID = "site-generated-id"
	e := storage.Record{SiteID: "site-generated-id", Kind: "event"}
	e.Data = json.RawMessage(`{"type":"health_changed","before":"unknown","after":"healthy"}`)
	if label := recordLabel(e, m.snapshot.Sites); label != "alpha.example.invalid" {
		t.Fatal(label)
	}
	if s := activitySummary(e); s != "Health observed: healthy" || strings.Contains(s, "recovered") {
		t.Fatal(s)
	}
	e.Data = json.RawMessage(`{"name":"removed.example","summary":"Site health changed"}`)
	if recordLabel(e, nil) != "removed.example" {
		t.Fatal("removed site lost its historical name")
	}
}

func TestActivityCombinesDeploymentsAndDeduplicatesTheSameHealthTransition(t *testing.T) {
	m := testDashboard(t)
	at := time.Now().UTC()
	for _, v := range []struct {
		kind string
		data any
	}{{"event", map[string]string{"type": "health_changed", "before": "healthy", "after": "critical"}}, {"change", map[string]string{"field": "health", "before": "healthy", "after": "critical"}}, {"deployment", map[string]string{"status": "completed", "commit": "abc"}}} {
		if _, err := m.source.Store.Add(m.ctx, m.source.Config.Host.ID, "alpha", v.kind, at, v.data); err != nil {
			t.Fatal(err)
		}
	}
	records, err := m.source.activity(m.ctx, "alpha", 100)
	if err != nil {
		t.Fatal(err)
	}
	count, deploy := 0, false
	for _, r := range records {
		if r.ObservedAt.Equal(at) {
			if r.Kind == "deployment" {
				deploy = true
			} else {
				count++
			}
		}
	}
	if count != 1 || !deploy {
		t.Fatal("duplicated a transition or omitted deployment", records)
	}
}
