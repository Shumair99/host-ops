package caddy

import "testing"

func TestNestedHostCannotOverrideMismatchedAncestor(t *testing.T) {
	data := []byte(`{"routes":[{"match":[{"host":["b.example"]}],"handle":[{"handler":"subroute","routes":[{"match":[{"host":["a.example"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"wrong:80"}]}]}]}]}]}`)
	u, found, err := Upstreams(data, "a.example")
	if err != nil || found || len(u) != 0 {
		t.Fatal(u, found, err)
	}
}

func TestRouteDoesNotBorrowSiblingUpstream(t *testing.T) {
	data := []byte(`{"srv":{"routes":[{"match":[{"host":["a.example"]}],"handle":[{"handler":"subroute","routes":[{"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"a:80"}]}]}]}]},{"match":[{"host":["b.example"]}],"handle":[{"handler":"reverse_proxy","upstreams":[{"dial":"b:3000"}]}]}]}}`)
	u, found, err := Upstreams(data, "a.example")
	if err != nil || !found || len(u) != 1 || u[0] != "a:80" {
		t.Fatalf("wrong route: %v %v %v", u, found, err)
	}
	_, found, _ = Upstreams(data, "missing.example")
	if found {
		t.Fatal("missing domain matched")
	}
}
