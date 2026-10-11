package readiness

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestAllImplementedReadinessRulesCanBeSatisfied(t *testing.T) {
	now := time.Now()
	r := model.Report{ObservedAt: now, Runtime: model.Runtime{PostureObserved: true, Status: "running", User: "1000", MemoryLimit: 1024, RestartPolicy: "unless-stopped", ImageID: "id", Commit: "abcdef1"}}
	r.Runtime.Processes = model.ProcessSnapshot{Observed: true, Complete: true, Items: []model.ProcessIdentity{{PID: 1, UID: 1000, Name: "app"}}}
	for _, layer := range []string{"runtime", "local_http", "proxy_route", "public_http", "tls"} {
		r.Checks = append(r.Checks, model.Check{Layer: layer, State: model.Pass})
	}
	got := Evaluate(config.Site{Runtime: config.Runtime{Type: "docker"}}, r, Evidence{External: true, Metrics: true, History: true, Backup: true, Restore: true}, now)
	if got.Score != 95 || got.MaxScore != 95 || got.Provisional {
		t.Fatal("readiness has an unsatisfiable rule", got)
	}
}
