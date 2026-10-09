package security

import (
	"context"
	"errors"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"strings"
	"testing"
	"time"
)

func TestPostureNeverTreatsStaleMetadataAsCurrent(t *testing.T) {
	site := config.Site{ID: "s", Runtime: config.Runtime{Type: "docker"}}
	r := model.Report{ObservedAt: time.Now(), Runtime: model.Runtime{Status: "running", User: "1000", ReadOnly: true, MemoryLimit: 1024, Image: "web@sha256:abc"}}
	for _, f := range Posture(site, r) {
		if f.State != model.Pass {
			t.Fatal(f)
		}
	}
	r.Runtime.Privileged = true
	r.Runtime.Mounts = []model.Mount{{Destination: "/var/run/docker.sock"}}
	failures := 0
	for _, f := range Posture(site, r) {
		if f.State == model.Fail {
			failures++
		}
	}
	if failures != 2 {
		t.Fatal("privilege/socket risks absent", failures)
	}
	r.ObservedAt = time.Now().Add(-time.Hour)
	findings := Posture(site, r)
	if len(findings) != 1 || findings[0].State != model.Unknown {
		t.Fatal(findings)
	}
}

type inventoryRunner struct {
	fail  bool
	calls []string
}

func (r *inventoryRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if r.fail {
		return nil, errors.New("denied-secret")
	}
	if name == "ss" {
		return []byte("LISTEN 0 128 127.0.0.1:8080 0.0.0.0:*\n"), nil
	}
	if len(args) > 0 && args[0] == "ps" {
		return []byte("{\"name\":\"owned\",\"state\":\"exited\"}\n{\"name\":\"old\",\"state\":\"exited\"}\n{\"name\":\"proxy\",\"state\":\"running\"}\ninvalid"), nil
	}
	return nil, errors.New("cannot inspect")
}
func TestSecurityInventoryDistinguishesOwnedStoppedWorkloadsFromPossibleOrphans(t *testing.T) {
	c := config.Config{Host: config.Host{ID: "h", CaddyContainer: "proxy"}, Sites: []config.Site{{ID: "s", Runtime: config.Runtime{Type: "docker", Container: "owned"}}}}
	runner := &inventoryRunner{}
	r := Inspect(context.Background(), c, nil, runner)
	if len(r.Containers) != 3 || len(r.PossibleOrphans) != 1 || r.PossibleOrphans[0].Name != "old" || len(r.Listening) != 1 {
		t.Fatal(r)
	}
	runner = &inventoryRunner{fail: true}
	r = Inspect(context.Background(), c, nil, runner)
	if len(r.Unavailable) != 2 || len(r.Containers) != 0 || strings.Contains(strings.Join(r.Unavailable, " "), "secret") {
		t.Fatal(r)
	}
}
