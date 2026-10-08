package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

func TestPersistenceLatestAndHostIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"healthy", "critical"} {
		if err = s.Reports(ctx, "h", []model.Report{{HostID: "h", SiteID: "s", Overall: state, ObservedAt: time.Now().UTC()}}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	latest, err := s.Latest(ctx, "h")
	if err != nil || len(latest) != 1 || latest[0].Overall != "critical" {
		t.Fatalf("latest %v %v", latest, err)
	}
	other, _ := s.Latest(ctx, "other")
	if len(other) != 0 {
		t.Fatal("host isolation broken")
	}
	history, err := s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	if err != nil || len(history) != 2 {
		t.Fatalf("history count %d: %v", len(history), err)
	}
}

func TestInventoryRetentionKeepsLatestAndOperationalEvidence(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().Add(-100 * 24 * time.Hour)
	for _, kind := range []string{"inventory", "deployment"} {
		for n := 0; n < 2; n++ {
			if _, err := s.Add(ctx, "h", "", kind, at.Add(time.Duration(n)*time.Minute), map[string]int{"n": n}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Prune(ctx, config.History{RetentionDays: 90}); err != nil {
		t.Fatal(err)
	}
	inventory, err := s.Query(ctx, "h", "", "inventory", time.Time{}, 100)
	if err != nil || len(inventory) != 1 {
		t.Fatal("inventory did not retain exactly its newest expired sample", inventory, err)
	}
	evidence, err := s.Query(ctx, "h", "", "deployment", time.Time{}, 100)
	if err != nil || len(evidence) != 2 {
		t.Fatal("operational evidence was removed by telemetry retention", evidence, err)
	}
}

func TestAddUniqueDeduplicatesReplayedHostHistory(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	at := time.Date(2026, 10, 8, 19, 12, 0, 123, time.UTC)
	sample := map[string]any{"host_id": "server-1", "cpu_percent": 12.5}
	inserted, err := store.AddUnique(context.Background(), "server-1", "", "host_metrics", at, sample)
	if err != nil || !inserted {
		t.Fatalf("first sample was not inserted: %v, %v", inserted, err)
	}
	inserted, err = store.AddUnique(context.Background(), "server-1", "", "host_metrics", at, sample)
	if err != nil || inserted {
		t.Fatalf("replayed sample was inserted twice: %v, %v", inserted, err)
	}
	records, err := store.Query(context.Background(), "server-1", "", "host_metrics", at.Add(-time.Second), 100)
	if err != nil || len(records) != 1 {
		t.Fatalf("expected one stored history sample, got %d: %v", len(records), err)
	}
}

func TestRetentionPrunesPersistentHistoryDedupKeys(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	old := time.Now().Add(-100 * 24 * time.Hour)
	for i := 0; i < 2; i++ {
		if _, err := store.AddUnique(ctx, "h", "", "host_metrics", old.Add(time.Duration(i)*time.Minute), map[string]int{"sample": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Prune(ctx, config.History{MetricsDays: 90}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB.QueryRow(`SELECT count(*) FROM history_imports`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pruned sample keys retained, or current key lost: count %d, err %v", count, err)
	}
}
