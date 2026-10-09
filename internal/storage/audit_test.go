package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
)

func auditStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOrderedMigrationPreservesLegacyHistoryAndRejectsFutureSchema(t *testing.T) {
	for _, version := range []int{0, 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			if err := protectHistory(path, false); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.Exec(migrations[0] + ` INSERT INTO dedup VALUES('h','deployment','old'); INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES('h','s','deployment','2026-01-01T00:00:00Z','{"id":"old"}'); PRAGMA user_version=` + fmt.Sprint(version))
			if err != nil {
				t.Fatal(err)
			}
			db.Close()
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			var gotVersion, count int
			if err = s.DB.QueryRow("PRAGMA user_version").Scan(&gotVersion); err != nil || gotVersion != schemaVersion {
				t.Fatal(gotVersion, err)
			}
			if err = s.DB.QueryRow("SELECT COALESCE(sum(samples),0) FROM blocks").Scan(&count); err != nil || count != 1 {
				t.Fatal("history lost", count, err)
			}
			if inserted, err := s.AddOnce(context.Background(), "h", "s", "deployment", "old", time.Now(), map[string]string{}); err != nil || inserted {
				t.Fatal("legacy dedup lost", inserted, err)
			}
			s.DB.Exec("PRAGMA user_version=999")
			s.Close()
			if future, err := Open(path); err == nil {
				future.Close()
				t.Fatal("future schema accepted")
			}
		})
	}
}

func TestUnchangedHealthKeepsFreshStateAndSparseHistory(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	start := time.Now().Add(-time.Minute)
	r := model.Report{HostID: "h", SiteID: "s", Overall: "healthy", ObservedAt: start, Checks: []model.Check{{Layer: "public_http", State: model.Pass, LatencyMS: 100}}}
	for i := 0; i < 4; i++ {
		r.ObservedAt = start.Add(time.Duration(i) * 20 * time.Second)
		r.Checks[0].LatencyMS++
		r.Checks[0].Summary = fmt.Sprintf("HTTP 200; %.0f ms", r.Checks[0].LatencyMS)
		if err := s.Reports(ctx, "h", []model.Report{r}); err != nil {
			t.Fatal(err)
		}
	}
	history, _ := s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	latest, _ := s.Latest(ctx, "h")
	if len(history) != 2 || len(latest) != 1 || !latest[0].ObservedAt.Equal(r.ObservedAt) {
		t.Fatal("heartbeat/history policy", len(history), latest)
	}
	r.ObservedAt = r.ObservedAt.Add(time.Second)
	r.Overall, r.Checks[0].State = "critical", model.Fail
	if err := s.Reports(ctx, "h", []model.Report{r}); err != nil {
		t.Fatal(err)
	}
	history, _ = s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	if len(history) != 3 {
		t.Fatal("transition was throttled")
	}
	r.ObservedAt = r.ObservedAt.Add(time.Second)
	if err := s.UpdateReports(ctx, "h", []model.Report{r}); err != nil {
		t.Fatal(err)
	}
	history, _ = s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	latest, _ = s.Latest(ctx, "h")
	if len(history) != 3 || !latest[0].ObservedAt.Equal(r.ObservedAt) {
		t.Fatal("inventory refresh duplicated history or lost freshness")
	}
	r.Overall, r.Checks[0].State = "warning", model.Warn
	if err := s.UpdateReports(ctx, "h", []model.Report{r}); err != nil {
		t.Fatal(err)
	}
	history, _ = s.Query(ctx, "h", "s", "health", time.Time{}, 100)
	if len(history) != 4 {
		t.Fatal("meaningful inventory transition was not retained immediately")
	}
}

func TestInventoryRecordsOnlyTopologyChanges(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	snapshot := discovery.Snapshot{HostID: "h", ObservedAt: time.Now(), Sources: []discovery.Source{{ID: "p", State: "ready", Mode: "active"}}, Routes: []discovery.Route{{ID: "r", SourceID: "p", Hosts: []string{"example.com"}, Kind: "response", Scheme: "https"}}}
	for i := 0; i < 5; i++ {
		snapshot.ObservedAt = snapshot.ObservedAt.Add(30 * time.Second)
		snapshot.Sources[0].ObservedAt = snapshot.ObservedAt
		if _, err := s.ReconcileInventory(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	records, _ := s.Query(ctx, "h", "", "inventory", time.Time{}, 100)
	current, _ := s.Inventory(ctx, "h")
	if len(records) != 1 || !current.Snapshot.ObservedAt.Equal(snapshot.ObservedAt) {
		t.Fatal("unchanged inventory repeated", len(records))
	}
	snapshot.Routes[0].Note = "Changed route"
	if _, err := s.ReconcileInventory(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	records, _ = s.Query(ctx, "h", "", "inventory", time.Time{}, 100)
	if len(records) != 2 {
		t.Fatal("topology change lost")
	}
}

func TestFiniteRetentionPrunesEvidenceAndLinkedDedup(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now()
	for _, kind := range []string{"metrics", "event", "backup", "restore", "deployment", "alert"} {
		for _, at := range []time.Time{now.Add(-14 * 24 * time.Hour), now} {
			if _, err := s.AddOnce(ctx, "h", "s", kind, at.String(), at, map[string]string{"id": at.String()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Prune(ctx, config.History{MetricsDays: 1, EvidenceDays: 7}); err != nil {
		t.Fatal(err)
	}
	var records, keys int
	s.DB.QueryRow("SELECT COALESCE(sum(samples),0) FROM blocks").Scan(&records)
	s.DB.QueryRow("SELECT count(*) FROM dedup").Scan(&keys)
	if records != 6 || keys != 6 {
		t.Fatal("finite retention did not prune linked keys", records, keys)
	}
}

func TestStorageBudgetPrunesHistoryProtectsOpenIncidentsAndWarns(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	if err := s.Prune(ctx, config.History{MaxDBMB: 16}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"incident-a", "incident-b"} {
		if _, err := s.Add(ctx, "h", "s", "incident", time.Now().Add(-400*24*time.Hour), map[string]string{"id": id, "status": "open"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 350; i++ {
		// High-entropy evidence must still exercise pressure after compression.
		payload := make([]byte, 64*1024)
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Add(ctx, "h", "s", "metrics", time.Now(), map[string]any{"payload": payload, "sequence": i}); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := s.Prune(ctx, config.History{MaxDBMB: 16}); err != nil {
		t.Fatal(err)
	}
	var size int64
	s.DB.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size)
	if size >= 16*1024*1024 {
		t.Fatal("storage budget exceeded", size)
	}
	data, err := s.State(ctx, "hostops", "", "history_pressure")
	if err != nil || !strings.Contains(string(data), "pruned_records") {
		t.Fatal("no storage pressure warning", string(data), err)
	}
	incidents, err := s.LatestEntities(ctx, "h", "incident")
	if err != nil || len(incidents) != 2 {
		t.Fatal("open incident evidence lost", incidents, err)
	}
	records, _ := s.Query(ctx, "h", "s", "metrics", time.Time{}, 1)
	if len(records) != 1 || !strings.Contains(string(records[0].Data), `"sequence":349`) {
		t.Fatal("current sample lost")
	}
}

func TestOneHourHistoryGrowthFixture(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	start := time.Now().Add(-time.Hour)
	snapshot := discovery.Snapshot{HostID: "h", Sources: []discovery.Source{{ID: "p", State: "ready", Mode: "active"}}}
	reports := []model.Report{}
	for i := 0; i < 21; i++ {
		id := fmt.Sprintf("site-%d", i)
		snapshot.Routes = append(snapshot.Routes, discovery.Route{ID: id, SourceID: "p", Hosts: []string{id + ".example"}, Kind: "response", Scheme: "https", Note: strings.Repeat("x", 5000)})
		r := model.Report{HostID: "h", SiteID: id, Overall: "healthy"}
		for _, layer := range []string{"runtime", "proxy_route", "local_http", "dns", "tcp", "tls", "public_http"} {
			r.Checks = append(r.Checks, model.Check{HostID: "h", SiteID: id, Layer: layer, State: model.Pass, Summary: "Observed passing layer"})
		}
		reports = append(reports, r)
	}
	for cycle := 0; cycle < 120; cycle++ {
		at := start.Add(time.Duration(cycle) * 30 * time.Second)
		snapshot.ObservedAt, snapshot.Sources[0].ObservedAt = at, at
		if _, err := s.ReconcileInventory(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
		for i := range reports {
			reports[i].ObservedAt = at
			if _, err := s.Add(ctx, "h", reports[i].SiteID, "metrics", at, map[string]any{"observed_at": at, "values": map[string]float64{"cpu_percent": 1, "memory_used_bytes": 100000000}}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Reports(ctx, "h", reports); err != nil {
			t.Fatal(err)
		}
		for _, offset := range []time.Duration{0, 15 * time.Second} {
			if _, err := s.Add(ctx, "h", "", "host_metrics", at.Add(offset), map[string]any{"observed_at": at.Add(offset), "values": map[string]float64{"cpu_percent": 1, "memory_percent": 38}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	var size, inventoryRows int64
	s.DB.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size)
	s.DB.QueryRow("SELECT COALESCE(sum(b.samples),0) FROM blocks b JOIN streams s ON b.stream_id=s.id WHERE s.kind='inventory'").Scan(&inventoryRows)
	t.Logf("one-hour fixture including fixed inventory/state: %.2f MiB, %d inventory row(s); use the full-day fixture rather than multiplying fixed overhead", float64(size)/(1024*1024), inventoryRows)
	if size > 1024*1024 || inventoryRows != 1 {
		t.Fatal("history growth regressed", size, inventoryRows)
	}
	var r map[string]any
	data, _ := s.State(ctx, "h", "site-0", "health")
	if json.Unmarshal(data, &r) != nil {
		t.Fatal("current health missing")
	}
}
