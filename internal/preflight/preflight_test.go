package preflight

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
)

func TestImpactAndMissingEvidenceBlockGreenRecommendation(t *testing.T) {
	c := config.Config{Host: config.Host{ID: "h", CaddyContainer: "proxy"}, Sites: []config.Site{{ID: "a", Proxy: config.Proxy{Domain: "a.example"}, Runtime: config.Runtime{Type: "docker", Container: "a"}}, {ID: "b", Proxy: config.Proxy{Domain: "b.example"}, Runtime: config.Runtime{Type: "static"}}}}
	impact, err := Dependency(c, "docker")
	if err != nil || len(impact.Affected) != 2 {
		t.Fatal(impact, err)
	}
	impact, _ = Dependency(c, "a")
	if len(impact.Affected) != 1 {
		t.Fatal(impact)
	}
	if Recommendation([]model.Check{{State: model.Unknown}}) == Recommendation([]model.Check{{State: model.Pass}}) {
		t.Fatal("unknown preflight returned green")
	}
}

func TestDiscoveredDependenciesIncludeEveryBackendAndProxy(t *testing.T) {
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "a", Discovered: true, Proxy: config.Proxy{Domain: "a.example"}, Dependencies: []string{"docker", "caddy", "proxy-a", "backend-1", "backend-2"}}, {ID: "b", Discovered: true, Proxy: config.Proxy{Domain: "b.example"}, Dependencies: []string{"systemd", "nginx", "proxy-b"}}}}
	for _, resource := range []string{"backend-2", "proxy-a", "caddy", "docker"} {
		impact, err := Dependency(c, resource)
		if err != nil || len(impact.Affected) != 1 || impact.Affected[0].ID != "a" {
			t.Fatalf("%s affects wrong sites: %+v %v", resource, impact, err)
		}
	}
	impact, err := Dependency(c, "nginx")
	if err != nil || len(impact.Affected) != 1 || impact.Affected[0].ID != "b" {
		t.Fatal(impact, err)
	}
}
