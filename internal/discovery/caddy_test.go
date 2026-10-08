package discovery

import "testing"

func TestCaddyPreservesDomainAndPathScope(t *testing.T) {
	data := []byte(`{"srv":{"listen":[":443"],"routes":[{"match":[{"host":["one.example"]}],"handle":[{"handler":"subroute","routes":[{"match":[{"path":["/api/*"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"api:3000"}]}]},{"handle":[{"handler":"vars","root":"/unusual/site"},{"handler":"file_server"}]}]}]},{"match":[{"host":["two.example"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"other:80"}]}]}]}}`)
	routes, err := ParseCaddy(data, "p", "proxy", []string{"docker:web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 {
		t.Fatalf("got %d routes", len(routes))
	}
	if routes[0].Hosts[0] != "one.example" || routes[0].Paths[0] != "/api/*" || routes[0].Upstreams[0] != "api:3000" {
		t.Fatal("nested scope was lost")
	}
	if routes[1].Kind != "static" || routes[1].Root != "/unusual/site" {
		t.Fatal("static root was lost")
	}
	if routes[2].Hosts[0] != "two.example" || routes[2].Upstreams[0] != "other:80" {
		t.Fatal("sibling route borrowed an upstream")
	}
}

func TestCaddyCurrentConfigurationOverridesPendingDeclaredRoutes(t *testing.T) {
	snapshot := Snapshot{HostID: "h", Sources: []Source{{ID: "active", State: "ready", Mode: "active"}, {ID: "declared", State: "ready", Mode: "configured"}}, Routes: []Route{{ID: "r", SourceID: "active", ProxyID: "p", Hosts: []string{"live.example"}}, {ID: "pending", SourceID: "declared", ProxyID: "p", Hosts: []string{"pending.example"}}}}
	r := Reconcile(snapshot, Registry{})
	if len(r.Sites) != 1 || r.Sites[0].Domain != "live.example" {
		t.Fatal("pending configuration masqueraded as loaded routing")
	}
}
