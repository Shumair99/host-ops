package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"github.com/charmbracelet/x/ansi"
)

func fictionalDNSFailure(m dashboard) dashboard {
	for i, r := range m.snapshot.Reports {
		if r.SiteID != "alpha" {
			continue
		}
		for j := range r.Checks {
			if r.Checks[j].Layer == "dns" || r.Checks[j].Layer == "public_http" {
				r.Checks[j].State, r.Checks[j].Code = model.Fail, "dns_not_found"
				r.Checks[j].Summary = "Fictional resolver found no DNS address"
				r.Checks[j].StatusCode, r.Checks[j].LatencyMS = 0, 0
			}
			if r.Checks[j].Layer == "tls" {
				r.Checks[j].State, r.Checks[j].Code, r.Checks[j].Summary = model.Skip, "blocked_by_dns", "Not probed: fictional DNS address lookup failed"
			}
		}
		r.Checks = append(r.Checks, model.Check{Layer: "registration", State: model.Warn, Code: "registration_expiry_elapsed", Summary: "Fictional registry expiration date has elapsed", ObservedAt: time.Now(), Details: []string{"Registry expiration: 2026-09-01T00:00:00Z", "Offline fixture only; no registry was contacted"}})
		r.Overall, r.Diagnoses = model.Overall(r.Checks), diagnostics.Diagnose(r)
		m.snapshot.Reports[i] = r
	}
	return m
}

func TestFindingsAreProminentWithoutClaimingDomainOwnership(t *testing.T) {
	m := fictionalDNSFailure(testDashboard(t))
	m.width, m.height, m.menuFocus = 144, 44, false
	for _, page := range []string{"overview", "sites", "issues", "site"} {
		m.page, m.siteID = page, ""
		if page == "site" {
			m.siteID = "alpha"
		}
		text := ansi.Strip(m.frame())
		if !strings.Contains(text, "Observed") || !strings.Contains(text, "Registry expiration") {
			t.Fatal(page, "buried the supported finding", text)
		}
		if strings.Contains(text, "ownership has expired") || strings.Contains(text, "Namecheap") {
			t.Fatal("invented ownership or registrar cause", text)
		}
	}
	r := model.Current(m.snapshot.Reports[0], time.Now().Add(time.Hour), time.Minute)
	if finding := primaryFinding(r); strings.Contains(finding, "Registry expiration") || !strings.Contains(finding, "unconfirmed") {
		t.Fatal("stale registration became a current diagnosis", finding)
	}
}

func TestAliasFindingsDoNotBorrowPrimaryDomainLocalEvidence(t *testing.T) {
	m := testDashboard(t)
	base := m.snapshot.Sites[0]
	alias := base
	alias.ID, alias.Proxy.Domain = "alpha-www", "www.alpha.example.invalid"
	m.snapshot.Sites = append(m.snapshot.Sites, alias)
	m.source.Config.Sites = append(m.source.Config.Sites, alias)
	m.snapshot.Reports = append(m.snapshot.Reports, model.Report{SiteID: alias.ID, Overall: "critical", Checks: []model.Check{{Layer: "local_http", State: model.Skip}, {Layer: "public_http", State: model.Fail, StatusCode: 502}}})
	r := groupReport(siteGroup{Primary: base, Children: []config.Site{alias}}, reportsByID(m.snapshot.Reports))
	for _, d := range reportFindings(r) {
		if strings.Contains(d.Finding, "local application responds") {
			t.Fatal("independent domains were causally combined", d)
		}
	}
}

func TestOverviewPinsCurrentFindingsWithoutBreakingMatrixOrRetainingResolvedNotices(t *testing.T) {
	m := fictionalDNSFailure(testDashboard(t))
	m.snapshot.Events = nil
	for _, width := range []int{38, 58, 122} {
		matrix := ansi.Strip(strings.Join(m.healthMatrix(width, 8), "\n"))
		if strings.Contains(matrix, "Registry expiration") || strings.Contains(matrix, "→") || !strings.Contains(matrix, "CRITICAL") {
			t.Fatal("matrix lost the status or gained diagnosis prose", matrix)
		}
	}
	feed := ansi.Strip(strings.Join(m.operationsFeed(122, 4, 3), "\n"))
	if !strings.Contains(feed, "Registry expiration date has elapsed") || !strings.Contains(feed, "No recorded changes") {
		t.Fatal("current notices depend on recent activity", feed)
	}
	m.snapshot.Events = []storage.Record{{Kind: "event", SiteID: "alpha", ObservedAt: time.Now(), Data: []byte(`{"summary":"An older incident"}`)}}
	feed = ansi.Strip(strings.Join(m.operationsFeed(122, 4, 3), "\n"))
	if strings.Index(feed, "Registry expiration") > strings.Index(feed, "An older incident") {
		t.Fatal("recent activity displaced an active notice", feed)
	}
	for i := range m.snapshot.Reports {
		for j := range m.snapshot.Reports[i].Checks {
			m.snapshot.Reports[i].Checks[j].State = model.Pass
		}
	}
	m.snapshot.Host.Values = nil
	feed = ansi.Strip(strings.Join(m.operationsFeed(122, 4, 3), "\n"))
	if strings.Contains(feed, "Registry expiration") || !strings.Contains(feed, "No active notices") || !strings.Contains(feed, "An older incident") {
		t.Fatal("resolved findings remained pinned or erased activity", feed)
	}
}

func TestIssuesFindingArrowBelongsToDomainAndWrappedLinesAlign(t *testing.T) {
	m := fictionalDNSFailure(testDashboard(t))
	for _, width := range []int{38, 122} {
		lines := strings.Split(ansi.Strip(strings.Join(m.findingDesk(width), "\n")), "\n")
		arrowCount := 0
		for i, line := range lines {
			if strings.Contains(line, "→") {
				arrowCount++
				if i == 0 || !strings.Contains(lines[i-1], "CRITICAL") || !strings.Contains(line, "→ Observed") {
					t.Fatal("explanation arrow is detached from its domain", lines)
				}
				if width == 38 && (i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "│     ") || strings.Contains(lines[i+1], "→")) {
					t.Fatal("wrapped finding does not align after the arrow", lines)
				}
			}
		}
		if arrowCount != 1 {
			t.Fatal("finding lacks a single domain-scoped explanation arrow", lines)
		}
	}
}

func TestContinuousBarsPreserveMissingColumnsAndZeroInMonochrome(t *testing.T) {
	full, zero := 100.0, 0.0
	row := barPlot([]*float64{&full, nil, &zero}, 1, 100)[0]
	colored := (theme{color: true}).plot(row, cyan)
	if ansi.StringWidth(colored) != 3 || strings.Count(colored, "\x1b[48;5;180m") != 1 {
		t.Fatal("a missing or measured-zero cell became a filled column", colored)
	}
	mono := (theme{}).plot(row, cyan)
	if mono != "█ ─" || strings.Contains(mono, "\x1b") {
		t.Fatal("monochrome lost zero or missing-data meaning", mono)
	}
	half := 50.0
	meter := (theme{color: true}).meter(&half, 10, cyan)
	if ansi.StringWidth(meter) != 10 || strings.ContainsAny(ansi.Strip(meter), "█░") || !strings.Contains(meter, "\x1b[48;5;180m") {
		t.Fatal("meter still depends on block-glyph spacing", meter)
	}
}
