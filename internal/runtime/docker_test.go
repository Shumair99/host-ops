package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

type fixtureRunner struct {
	state  string
	denied bool
}

func (f fixtureRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if f.denied {
		return nil, errors.New("denied")
	}
	if args[0] == "info" {
		return []byte("28.0"), nil
	}
	return []byte(f.state), nil
}

func TestRuntimeDoesNotConfuseRunningWithHealthy(t *testing.T) {
	s := config.Site{ID: "site", HostID: "host", Runtime: config.Runtime{Container: "site"}}
	for _, tc := range []struct{ state, want string }{
		{`{"status":"running","health":"healthy"}`, "healthy"},
		{`{"status":"running","health":"unhealthy"}`, "critical"},
		{`{"status":"exited","health":"healthy","exit_code":1}`, "critical"},
		{`{"status":"running","health":"none"}`, "warning"},
	} {
		_, checks := InspectDocker(context.Background(), fixtureRunner{state: tc.state}, s)
		if got := model.Overall(checks); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.state, got, tc.want)
		}
	}
	_, checks := InspectDocker(context.Background(), fixtureRunner{denied: true}, s)
	if model.Overall(checks) != "unknown" {
		t.Fatal("access failure must not become healthy or a confirmed stopped container")
	}
	for _, forbidden := range []string{".Config.Env", ".State.Health.Log"} {
		if strings.Contains(inspectFormat, forbidden) {
			t.Fatal("secret-bearing inspect field requested")
		}
	}
}
