package preflight

import (
	"context"
	"errors"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"testing"
	"time"
)

type deniedRunner struct{}

func (deniedRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("permission denied")
}
func TestRebootPreflightRequiresIndependentFreshSafetyEvidence(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := config.Config{Host: config.Host{ID: "h", CaddyContainer: "proxy"}, Sites: []config.Site{{ID: "s", HostID: "h", Runtime: config.Runtime{Type: "docker", Container: "web"}}}}
	r, err := Assess(context.Background(), c, "reboot", []model.Report{{SiteID: "s", ObservedAt: time.Now().Add(-time.Hour), Overall: "healthy", Runtime: model.Runtime{Status: "running", RestartPolicy: "unless-stopped"}}}, store, deniedRunner{})
	if err != nil || r.Overall == "healthy" {
		t.Fatal(r, err)
	}
	states := map[string]model.State{}
	for _, check := range r.Checks {
		states[check.Layer] = check.State
	}
	for _, layer := range []string{"docker_boot", "caddy_boot", "site_health", "host_capacity", "restore_evidence"} {
		if states[layer] != model.Unknown {
			t.Fatal(layer, states)
		}
	}
	if _, err := Assess(context.Background(), c, "unregistered", nil, store, deniedRunner{}); err == nil {
		t.Fatal("unknown maintenance target accepted")
	}
}
