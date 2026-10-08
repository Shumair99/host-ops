package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
)

func TestSharedComposeAliasRemainsUnresolvedUntilUniquelyIdentified(t *testing.T) {
	snapshot := Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []Source{{ID: "p", State: "ready", Mode: "active"}}}
	for i := 0; i < 5; i++ {
		snapshot.Workloads = append(snapshot.Workloads, Workload{ID: fmt.Sprint(i), Name: fmt.Sprintf("tenant-%d-app", i), Networks: map[string]Network{"docker:shared": {Aliases: []string{"app"}, IP: fmt.Sprintf("172.18.0.%d", i+2)}}})
	}
	snapshot.Workloads[0].Networks["docker:private"] = Network{Aliases: []string{"app"}}
	route := Route{ID: "r", SourceID: "p", Kind: "proxy", Scheme: "https", Hosts: []string{"example.com"}, Upstreams: []string{"app:8080"}, Networks: []string{"docker:private", "docker:shared"}}
	snapshot.Routes = []Route{route}
	if ids := ResolveUpstream("app:8080", route, snapshot); len(ids) != 0 {
		t.Fatal("ambiguous alias attributed to tenants", ids)
	}
	registry := Reconcile(snapshot, Registry{})
	if len(registry.Sites) != 1 || registry.Sites[0].State != "unresolved" || len(registry.Sites[0].WorkloadIDs) != 0 {
		t.Fatal(registry.Sites)
	}
	for _, upstream := range []string{"tenant-0-app:8080", "172.18.0.2:8080"} {
		if ids := ResolveUpstream(upstream, route, snapshot); len(ids) != 1 || ids[0] != "0" {
			t.Fatal("exact identity not resolved", ids)
		}
	}
	if ids := ResolveUpstream("app:8080", Route{Networks: []string{"docker:private"}}, snapshot); len(ids) != 1 || ids[0] != "0" {
		t.Fatal("unique private alias not resolved", ids)
	}
}

func TestManualHealthSettingsPreserveDerivedPublicURLAndMultiBackendIsNotAChosenRuntime(t *testing.T) {
	registry := Registry{Sites: []Site{{ID: "found", Domain: "example.com", WorkloadIDs: []string{"a", "b"}, Routes: []Route{{Scheme: "https", Kind: "proxy", Upstreams: []string{"a:8080", "b:8080"}}}}}, Snapshot: Snapshot{Workloads: []Workload{{ID: "a", Type: "docker", Name: "first"}, {ID: "b", Type: "docker", Name: "second"}}}}
	base := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "custom", Name: "Website", Proxy: config.Proxy{Domain: "example.com"}, Health: config.Health{Path: "/ready", ExpectedStatus: 204, TimeoutSeconds: 3}}}}
	got := ConfigSites(registry, base).Sites[0]
	if got.Health.PublicURL != "https://example.com/" || got.Health.Path != "/ready" || got.Health.ExpectedStatus != 204 || got.ID != "custom" || got.Runtime.Type != "unresolved" || got.Runtime.Container != "" {
		t.Fatal(got)
	}
	base.Sites[0].Health.PublicURL = "https://example.com/ready"
	if got := ConfigSites(registry, base).Sites[0]; got.Health.PublicURL != base.Sites[0].Health.PublicURL {
		t.Fatal("explicit URL overwritten")
	}
}

func FuzzProxyConfigurationParsers(f *testing.F) {
	f.Add([]byte(`{"apps":{"http":{"servers":{"s":{"listen":[":443"],"routes":[{"match":[{"host":["example.com"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"app:80"}]}]}]}}}}}`))
	f.Add([]byte("server { listen 443; server_name example.com; location / { proxy_pass http://app:80; } }"))
	f.Add([]byte(`<VirtualHost *:443>\nServerName example.com\nDocumentRoot /srv/site\n</VirtualHost>`))
	f.Add([]byte(`{"http":{"routers":{"web":{"rule":"Host(` + "`example.com`" + `)"}}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			t.Skip()
		}
		ParseCaddy(data, "s", "p", nil)
		ParseNginx(data, "s", "p", nil)
		ParseApache(data, "s", "p", nil)
		ParseTraefik(data, "s", "p", nil)
	})
}
