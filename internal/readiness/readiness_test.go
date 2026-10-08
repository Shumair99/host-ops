package readiness

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestUnknownEvidenceCannotEarnReadinessPoints(t *testing.T) {
	now := time.Now()
	site := config.Site{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker"}}
	a := Evaluate(site, model.Report{}, Evidence{}, now)
	if a.Score != 0 || !a.Provisional {
		t.Fatal(a)
	}
	r := model.Report{ObservedAt: now, Runtime: model.Runtime{Status: "running", User: "1000", MemoryLimit: 1024, RestartPolicy: "unless-stopped", ImageID: "id", Image: "site:v1", Commit: "abcdef1"}, Checks: []model.Check{{Layer: "runtime", State: model.Pass}, {Layer: "local_http", State: model.Pass}, {Layer: "proxy_route", State: model.Pass}, {Layer: "public_http", State: model.Pass}, {Layer: "tls", State: model.Pass}}}
	a = Evaluate(site, r, Evidence{External: true, Metrics: true, History: true, Backup: true}, now)
	if a.Score != 85 {
		t.Fatal("restore and rollback should remain unverified", a.Score)
	}
	r.ObservedAt = now.Add(-time.Hour)
	if Evaluate(site, r, Evidence{}, now).Score != 0 {
		t.Fatal("stale runtime earned points")
	}
}

func TestRootWithGroupCannotPassUserRule(t *testing.T) {
	now := time.Now()
	site := config.Site{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker"}}
	for _, user := range []string{"root:root", "0:1000"} {
		a := Evaluate(site, model.Report{ObservedAt: now, Runtime: model.Runtime{Status: "running", User: user}}, Evidence{}, now)
		for _, rule := range a.Rules {
			if rule.ID == "user" && rule.State != model.Fail {
				t.Fatal(rule)
			}
		}
	}
}
