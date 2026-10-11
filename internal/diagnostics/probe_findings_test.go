package diagnostics

import (
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/model"
)

func TestDiagnosesKeepDNSCausesAndOwnershipUnconfirmed(t *testing.T) {
	r := model.Report{Checks: []model.Check{{Layer: "runtime", State: model.Pass}, {Layer: "proxy_route", State: model.Pass}, {Layer: "dns", State: model.Fail, Code: "dns_not_found", Summary: "no DNS address"}, {Layer: "public_http", State: model.Fail, Code: "dns_not_found", Summary: "no DNS address"}, {Layer: "tls", State: model.Skip, Code: "blocked_by_dns"}}}
	findings := Diagnose(r)
	if len(findings) != 1 || findings[0].Layer != "dns" || !strings.Contains(findings[0].Investigation, "does not establish") {
		t.Fatal(findings)
	}
	r.Checks = append(r.Checks, model.Check{Layer: "registration", State: model.Warn, Code: "registration_expiry_elapsed", Summary: "date elapsed", Details: []string{"Registry expiration: 2000-01-01T00:00:00Z"}})
	findings = Diagnose(r)
	if len(findings) != 2 || findings[0].Layer != "registration" || !strings.Contains(findings[0].Investigation, "does not prove loss of ownership") || len(findings[0].Evidence) < 2 {
		t.Fatal(findings)
	}
}

func TestGatewayStatusDoesNotProveCaddyConfigurationIsWrong(t *testing.T) {
	r := model.Report{Checks: []model.Check{{Layer: "local_http", State: model.Pass}, {Layer: "public_http", State: model.Fail, StatusCode: 502}}}
	d := Diagnose(r)
	if len(d) != 1 || d[0].Confidence != "medium" || !strings.Contains(d[0].Investigation, "does not prove") {
		t.Fatal(d)
	}
	r.Checks[0].State = model.Skip
	d = Diagnose(r)
	if len(d) != 1 || d[0].Layer != "http" || strings.Contains(d[0].Finding, "Caddy") {
		t.Fatal(d)
	}
}

func TestIndependentRuntimeFailuresCannotBeOverwrittenByPassingSibling(t *testing.T) {
	r := model.Report{Checks: []model.Check{{Layer: "runtime", State: model.Fail, Summary: "a exited"}, {Layer: "runtime", State: model.Pass, Summary: "b running"}, {Layer: "public_http", State: model.Fail, StatusCode: 503}}}
	d := Diagnose(r)
	if len(d) != 1 || d[0].Layer != "runtime" || !strings.Contains(strings.Join(d[0].Evidence, " "), "a exited") {
		t.Fatal(d)
	}
	for i := range r.Checks {
		r.Checks[i].State = model.Unknown
	}
	for _, finding := range Diagnose(r) {
		if finding.Confidence != "unconfirmed" {
			t.Fatal("unavailable evidence confirmed a fault", finding)
		}
	}
}

func TestTLSAndHTTPFailuresUseTypedEvidenceAndDeduplicateSymptoms(t *testing.T) {
	for _, tc := range []struct {
		checks            []model.Check
		layer, confidence string
	}{
		{[]model.Check{{Layer: "public_http", State: model.Fail, Code: "tls_certificate_expired", Summary: "Presented certificate has expired"}}, "tls", "high"},
		{[]model.Check{{Layer: "public_http", State: model.Fail, Code: "redirect_limit", Summary: "HTTP redirect limit exceeded"}}, "http", "high"},
		{[]model.Check{{Layer: "public_http", State: model.Fail, Code: "probe_timeout"}}, "http", "unconfirmed"},
		{[]model.Check{{Layer: "tcp", State: model.Fail, Code: "connection_refused"}, {Layer: "public_http", State: model.Fail, Code: "connection_refused"}}, "public_network", "high"},
	} {
		d := Diagnose(model.Report{Checks: tc.checks})
		if len(d) != 1 || d[0].Layer != tc.layer || d[0].Confidence != tc.confidence {
			t.Fatal(tc, d)
		}
	}
}
