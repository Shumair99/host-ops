package tui

import (
	"context"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
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

func TestDisconnectedCollectorDoesNotRenderRetainedRuntimeAsPassing(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.ReconcileInventory(ctx, discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []discovery.Source{{ID: "p", State: "ready"}}, Routes: []discovery.Route{{ID: "r", SourceID: "p", Hosts: []string{"example.com"}}}})
	if err != nil {
		t.Fatal(err)
	}
	source := Source{Config: config.Config{Host: config.Host{ID: "h"}}, Store: store, Live: true}
	snapshot, err := source.Base(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Reports(ctx, "h", []model.Report{{HostID: "h", SiteID: snapshot.Sites[0].ID, ObservedAt: time.Now(), Overall: "healthy", Checks: []model.Check{{Layer: "runtime", State: model.Pass}, {Layer: "proxy_route", State: model.Pass}}}}); err != nil {
		t.Fatal(err)
	}
	if err = store.Transition(ctx, "h", "", "connection", agent.Connection{Connected: false, ObservedAt: time.Now()}, "", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	snapshot, err = source.Base(ctx)
	if err != nil || snapshot.Reports[0].Overall == "healthy" || snapshot.Reports[0].Checks[0].State != model.Unknown {
		t.Fatal("disconnected collector displayed a fresh pass", err)
	}
}
