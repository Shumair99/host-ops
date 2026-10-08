package tui

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestDashboardListsAutomaticSitesWithoutManifestEntries(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := config.Config{Host: config.Host{ID: "h"}}
	_, err = store.ReconcileInventory(context.Background(), discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []discovery.Source{{ID: "p", State: "ready", Mode: "active"}}, Routes: []discovery.Route{{ID: "r", SourceID: "p", Hosts: []string{"new.example"}, Scheme: "https", Kind: "static", Root: "/unusual/path"}}})
	if err != nil {
		t.Fatal(err)
	}
	m := newDashboard(context.Background(), Source{Config: c, Store: store}, Options{Interval: 30 * time.Second})
	m.snapshot, err = m.source.Base(m.ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.page = "sites"
	if len(m.filteredSites()) != 1 || !strings.Contains(strings.Join(m.liveLines(), "\n"), "new.example") {
		t.Fatal("dashboard requires a manual site entry")
	}
}
