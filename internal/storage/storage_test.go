package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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
