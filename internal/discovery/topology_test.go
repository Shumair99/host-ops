package discovery

import (
	"github.com/Shumair99/host-ops/internal/config"
	"testing"
)

func TestWorkloadDependenciesFollowDatabaseConnectionsWithoutCrossingProxyNeighbours(t *testing.T) {
	s := Snapshot{Connections: []Connection{{From: "proxy", To: "a", Kind: "established"}, {From: "proxy", To: "b", Kind: "established"}, {From: "a", To: "db", Kind: "declared"}}}
	got := WorkloadDependencies(s, []string{"a"})
	if len(got) != 2 || got[0] != "a" || got[1] != "db" {
		t.Fatal(got)
	}
}

func TestHostOnlyCaddyRedirectDoesNotInheritFallbackBackend(t *testing.T) {
	data := []byte("{\"srv\":{\"listen\":[\":443\"],\"routes\":[{\"match\":[{\"host\":[\"example.com\",\"www.example.com\"]}],\"handle\":[{\"handler\":\"subroute\",\"routes\":[{\"match\":[{\"host\":[\"www.example.com\"]}],\"handle\":[{\"handler\":\"static_response\",\"status_code\":308,\"headers\":{\"Location\":[\"https://example.com{http.request.uri}\"]}}]},{\"handle\":[{\"handler\":\"reverse_proxy\",\"upstreams\":[{\"dial\":\"app:80\"}]}]}]}]}]}}")
	routes, err := ParseCaddy(data, "source", "proxy", nil)
	if err != nil || len(routes) != 2 {
		t.Fatal(routes, err)
	}
	if routes[0].Kind != "redirect" || len(routes[1].Hosts) != 1 || routes[1].Hosts[0] != "example.com" {
		t.Fatal(routes)
	}
}

func TestRedirectImpactFollowsDestinationDatabaseWithoutInventingARuntime(t *testing.T) {
	reg := Registry{Snapshot: Snapshot{HostID: "h", Workloads: []Workload{{ID: "app", Type: "docker", Name: "app"}, {ID: "db", Type: "docker", Name: "db"}}, Connections: []Connection{{From: "app", To: "db", Kind: "declared"}, {From: "app", Address: "192.0.2.5", Port: 443, Kind: "peer"}}}, Sites: []Site{
		{ID: "main", Domain: "example.com", State: "active", WorkloadIDs: []string{"app"}, Routes: []Route{{Kind: "proxy", Scheme: "https", Upstreams: []string{"app:80"}}}},
		{ID: "alias", Domain: "www.example.com", State: "active", Routes: []Route{{Kind: "redirect", Scheme: "https", RedirectTo: "https://example.com{uri}"}}},
		{ID: "other", Domain: "other.example", State: "active"},
	}}
	c := ConfigSites(reg, config.Config{Host: config.Host{ID: "h"}})
	for _, site := range c.Sites {
		deps := map[string]bool{}
		for _, dep := range site.Dependencies {
			deps[dep] = true
		}
		if site.ID == "main" || site.ID == "alias" {
			if !deps["db"] || !deps["endpoint:192.0.2.5:443"] {
				t.Fatal(site)
			}
		}
		if site.ID == "alias" && site.Runtime.Container != "" {
			t.Fatal("redirect acquired a fake app runtime", site)
		}
		if site.ID == "other" && deps["db"] {
			t.Fatal("unrelated site acquired database dependency", site)
		}
	}
}
