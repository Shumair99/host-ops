package diagnostics

import (
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
)

func TestDiagnosisNamesConfirmedLayersAndKeepsMissingEvidenceUnconfirmed(t *testing.T) {
	for _, tc := range []struct {
		layer string
		want  string
	}{{"runtime", "runtime"}, {"proxy", "proxy"}, {"proxy_route", "proxy"}, {"dns", "dns"}, {"tls", "tls"}} {
		findings := Diagnose(model.Report{Checks: []model.Check{{Layer: tc.layer, State: model.Fail, Summary: "measured failure"}}})
		if len(findings) != 1 || findings[0].Layer != tc.want || len(findings[0].Evidence) == 0 {
			t.Fatal(tc, findings)
		}
	}
	findings := Diagnose(model.Report{Checks: []model.Check{{Layer: "local_http", State: model.Pass}, {Layer: "public_http", State: model.Fail}}})
	if len(findings) != 1 || findings[0].Layer != "public_network" {
		t.Fatal(findings)
	}
	findings = Diagnose(model.Report{Checks: []model.Check{{Layer: "runtime", State: model.Unknown}}})
	if len(findings) != 1 || findings[0].Confidence != "unconfirmed" {
		t.Fatal(findings)
	}
}
