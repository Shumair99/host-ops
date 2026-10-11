package readiness

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestOnlyConfirmedWWWRedirectsShareAReadinessAssessment(t *testing.T) {
	now := time.Now()
	sites := []config.Site{
		{ID: "base", Proxy: config.Proxy{Domain: "example.com"}},
		{ID: "www", Proxy: config.Proxy{Domain: "www.example.com", RedirectTo: "example.com"}},
		{ID: "analytics", Proxy: config.Proxy{Domain: "analytics.example.com"}},
	}
	a := []Assessment{{SiteID: "base", Name: "example.com", ObservedAt: now, Score: 50, MaxScore: 95}, {SiteID: "www", Name: "www.example.com", ObservedAt: now, Score: 25, MaxScore: 50}, {SiteID: "analytics", Name: "analytics.example.com", ObservedAt: now}}
	reports := []model.Report{{SiteID: "www", ObservedAt: now, Checks: []model.Check{{Layer: "public_http", State: model.Fail, Summary: "Redirect unreachable"}}}}
	out := CombineAliases(sites, a, reports)
	if len(out) != 2 || out[0].Score != 50 || len(out[0].Aliases) != 1 || out[0].Rules[0].State != model.Fail || out[1].SiteID != "analytics" {
		t.Fatal(out)
	}
}

func TestApexRedirectKeepsCanonicalNameAndFailedAddressEvidence(t *testing.T) {
	now := time.Now()
	sites := []config.Site{
		{ID: "base", Proxy: config.Proxy{Domain: "example.com", RedirectTo: "www.example.com"}},
		{ID: "www", Proxy: config.Proxy{Domain: "www.example.com"}},
	}
	assessments := []Assessment{{SiteID: "base", ObservedAt: now}, {SiteID: "www", ObservedAt: now, Score: 55, MaxScore: 90}}
	reports := []model.Report{{SiteID: "base", ObservedAt: now, Checks: []model.Check{{Layer: "public_http", State: model.Fail, Summary: "Request failed"}, {Layer: "tls", State: model.Unknown, Summary: "Unavailable"}}}}
	out := CombineAliases(sites, assessments, reports)
	if len(out) != 1 || out[0].Name != "example.com" || out[0].Aliases[0] != "www.example.com" || out[0].Score != 55 || out[0].Rules[0].State != model.Fail {
		t.Fatal(out)
	}
}
