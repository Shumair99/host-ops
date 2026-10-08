package discovery

import (
	"testing"
	"time"
)

func TestDiscoveryKeepsDownSitesAndPreservesUnavailableSources(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snapshot := Snapshot{HostID: "h", ObservedAt: at, Sources: []Source{{ID: "proxy", State: "ready", Mode: "active"}}, Routes: []Route{{ID: "r", SourceID: "proxy", Hosts: []string{"example.com"}, Scheme: "https", Kind: "proxy", Upstreams: []string{"missing:80"}}}}
	r := Reconcile(snapshot, Registry{})
	if len(r.Sites) != 1 || r.Sites[0].State != "unresolved" {
		t.Fatalf("down backend lost its site: %+v", r.Sites)
	}
	failed := Snapshot{HostID: "h", ObservedAt: at.Add(time.Minute), Sources: []Source{{ID: "proxy", State: "unavailable", Mode: "active"}}}
	r = Reconcile(failed, r)
	if len(r.Sites) != 1 || r.Sites[0].State != "stale" || len(r.Sites[0].Routes) != 1 {
		t.Fatal("unavailable source erased inventory")
	}
	removed := Snapshot{HostID: "h", ObservedAt: at.Add(2 * time.Minute), Sources: []Source{{ID: "proxy", State: "ready", Mode: "active"}}}
	r = Reconcile(removed, r)
	if r.Sites[0].State == "removed" {
		t.Fatal("one successful scan removed a site")
	}
	removed.ObservedAt = removed.ObservedAt.Add(30 * time.Second)
	r = Reconcile(removed, r)
	if r.Sites[0].State != "removed" {
		t.Fatal("confirmed disappearance was not archived")
	}
}

func TestUpstreamAliasesAreScopedToProxyNetwork(t *testing.T) {
	snapshot := Snapshot{Workloads: []Workload{{ID: "a", Name: "first", Networks: map[string]Network{"one": {Aliases: []string{"web"}}}}, {ID: "b", Name: "second", Networks: map[string]Network{"two": {Aliases: []string{"web"}}}}}}
	ids := ResolveUpstream("web:80", Route{Networks: []string{"two"}}, snapshot)
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("cross-network alias borrowed another app: %v", ids)
	}
}

func TestWildcardIsCoverageNotInventedDomain(t *testing.T) {
	r := Reconcile(Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []Source{{ID: "p", State: "ready"}}, Routes: []Route{{ID: "r", SourceID: "p", Hosts: []string{"*.example.com"}}}}, Registry{})
	if len(r.Sites) != 0 || len(r.Patterns) != 1 {
		t.Fatal("wildcard became an invented site")
	}
}
