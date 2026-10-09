package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
)

func TestBlockCodecExactBytesAndCorruption(t *testing.T) {
	values := []string{`{"x":1.2300,"a":null,"s":"a\\b\"c"}`, `{"x":1.2301,"a":null,"s":"a\\b\"d"}`, `{"new":[true,false],"x":-0}`, ` { "empty": [] } `, `{"x":1e-9,"large":9007199254740993}`}
	at := time.Date(2026, 10, 9, 0, 0, 0, 123456789, time.UTC)
	var samples []observation
	for i, v := range values {
		samples = append(samples, observation{id: int64(i+1) * 7, at: at.Add(time.Duration(i) * -time.Second), data: []byte(v)})
	}
	raw := encodeFrames(samples)
	for _, encoding := range []int{0, 1} {
		data := raw
		if encoding == 1 {
			var err error
			data, err = packBlock(raw)
			if err != nil {
				t.Fatal(err)
			}
		}
		decoded, err := unpackBlock(data, encoding)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeFrames(decoded, len(samples))
		if err != nil {
			t.Fatal(err)
		}
		for i := range got {
			if got[i].id != samples[i].id || !got[i].at.Equal(samples[i].at) || !bytes.Equal(got[i].data, samples[i].data) {
				t.Fatalf("lost observation %d", i)
			}
		}
		if encoding == 1 {
			data[len(data)-1] ^= 1
			if _, err := unpackBlock(data, 1); err == nil {
				t.Fatal("checksum corruption accepted")
			}
		}
	}
	for _, bad := range [][]byte{nil, raw[:len(raw)-1], append(bytes.Clone(raw), 0)} {
		if _, err := decodeFrames(bad, len(samples)); err == nil {
			t.Fatal("malformed block accepted")
		}
	}
}

func TestCompactPageSizeForMemoryAndDisk(t *testing.T) {
	for _, path := range []string{":memory:", filepath.Join(t.TempDir(), "data", "history.db")} {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var pageSize int
		err = s.DB.QueryRow(`PRAGMA page_size`).Scan(&pageSize)
		s.Close()
		if err != nil || pageSize != 1024 {
			t.Fatal(path, pageSize, err)
		}
	}
}

func TestCompactQueriesPreserveGapsOrderIsolationAndReplay(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	at := time.Now().UTC().Add(-time.Hour)
	var expected []Record
	for i := 0; i < 400; i++ {
		stamp := at.Add(time.Duration(i) * time.Second)
		if i >= 160 {
			stamp = stamp.Add(10 * time.Minute)
		}
		if i == 390 {
			stamp = at.Add(5 * time.Second)
		}
		value := map[string]any{"v": i, "observed_at": stamp, "nested": []any{true, nil, "same"}}
		id, err := s.Add(ctx, "h", "s", "metrics", stamp, value)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(value)
		expected = append(expected, Record{ID: id, ObservedAt: stamp, Data: data})
		if _, err = s.Add(ctx, "other", "s", "metrics", stamp, value); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Query(ctx, "h", "s", "metrics", time.Time{}, 1000)
	if err != nil || len(got) != len(expected) {
		t.Fatal(len(got), err)
	}
	for i, r := range got {
		want := expected[len(expected)-1-i]
		if r.ID != want.ID || !r.ObservedAt.Equal(want.ObservedAt) || !bytes.Equal(r.Data, want.Data) {
			t.Fatalf("observation %d changed", i)
		}
	}
	window, err := s.Query(ctx, "h", "s", "metrics", at.Add(159*time.Second), 7)
	if err != nil || len(window) != 7 || window[0].ID != expected[399].ID {
		t.Fatal(window, err)
	}
	first, err := s.Oldest(ctx, "h", "s", "metrics", at.Add(159*time.Second))
	if err != nil || first == nil || first.ID != expected[159].ID {
		t.Fatal(first, err)
	}
	inserted, err := s.AddUnique(ctx, "h", "s", "metrics", expected[2].ObservedAt, map[string]int{"v": 2})
	if err != nil || inserted {
		t.Fatal("sealed replay duplicated", inserted, err)
	}
	latest, err := s.Query(ctx, "h", "", "", time.Time{}, 1)
	if err != nil || latest[0].ID != expected[399].ID {
		t.Fatal(latest, err)
	}
}

func TestCompactPruneBoundaryAndReopenActiveBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "history.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 150; i++ {
		at := now.Add(-48*time.Hour + time.Duration(i)*time.Hour/100)
		if i >= 100 {
			at = now.Add(-time.Hour + time.Duration(i)*time.Second)
		}
		if _, err = s.AddUnique(ctx, "h", "", "host_metrics", at, map[string]int{"sample": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Prune(ctx, config.History{MetricsDays: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query(ctx, "h", "", "host_metrics", time.Time{}, 1000)
	if err != nil || len(got) != 50 {
		t.Fatal(len(got), err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if inserted, err := s.AddUnique(ctx, "h", "", "host_metrics", got[0].ObservedAt, map[string]int{"sample": 149}); err != nil || inserted {
		t.Fatal(inserted, err)
	}
	if _, err = s.Add(ctx, "h", "", "host_metrics", now, map[string]int{"sample": 150}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Query(ctx, "h", "", "host_metrics", time.Time{}, 1000)
	if err != nil || len(got) != 51 {
		t.Fatal(len(got), err)
	}
}

func TestCompactPruneRefreshesTailChangedAfterCandidateSelection(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	policy := config.History{MetricsDays: 1}.Defaults()
	for i, at := range []time.Time{now.Add(-49 * time.Hour), now.Add(-48 * time.Hour), now.Add(-time.Hour)} {
		if _, err := s.Add(ctx, "h", "s", "metrics", at, map[string]int{"sample": i}); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := s.retentionBlocks(ctx, false, policy)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	if _, err = s.Add(ctx, "h", "s", "metrics", now, map[string]int{"sample": 3}); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	removed, err := pruneBlock(ctx, tx, candidates[0], now.Add(-24*time.Hour))
	if err != nil || removed != 2 {
		t.Fatal(removed, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query(ctx, "h", "s", "metrics", time.Time{}, 1000)
	if err != nil || len(got) != 2 || string(got[0].Data) != `{"sample":3}` {
		t.Fatal(got, err)
	}
}

func TestCompactMigrationRollbackPreservesOriginal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	if err := protectHistory(path, false); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:3] {
		if _, err = db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO records VALUES(9,'h','s','health','bad timestamp','{}'); PRAGMA user_version=3`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("invalid legacy data accepted")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatal(version, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM records WHERE id=9`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCompactMigrationRejectsZeroIDWithoutReassigningIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zero.db")
	if err := protectHistory(path, false); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:3] {
		if _, err = db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO records VALUES(0,'h','s','metrics','2026-10-09T00:00:00Z','{"v":1}'); PRAGMA user_version=3`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("unsupported legacy ID silently reassigned")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM records WHERE id=0`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCompactVersionThreeMigrationPreservesAllEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	if err := protectHistory(path, false); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, migration := range migrations[:3] {
		if _, err = db.Exec(migration); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC().Add(-time.Hour)
	expected := []observation{}
	seed, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 300; i++ {
		stamp := at.Add(time.Duration(i) * time.Second)
		data, _ := json.Marshal(map[string]any{"observed_at": stamp, "v": float64(i) / 17, "unavailable": []string{"optional"}})
		id := int64(i+1) * 3
		if _, err = seed.Exec(`INSERT INTO records VALUES(?,'h','s','metrics',?,?)`, id, stamp.Format(time.RFC3339Nano), string(data)); err != nil {
			seed.Rollback()
			t.Fatal(err)
		}
		expected = append(expected, observation{id: id, at: stamp, data: data})
	}
	if err = seed.Commit(); err != nil {
		t.Fatal(err)
	}
	state := `{"host_id":"h","site_id":"s","overall":"healthy","observed_at":"2026-10-09T00:00:00Z"}`
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO records VALUES(901,'h','s','incident',?,'{"id":"open-a","status":"open"}')`, []any{at.Format(time.RFC3339Nano)}},
		{`INSERT INTO records VALUES(902,'h','s','deployment',?,'{"id":"deploy-a"}')`, []any{at.Format(time.RFC3339Nano)}},
		{`INSERT INTO dedup VALUES('h','deployment','deploy-key',?,902)`, []any{at.Format(time.RFC3339Nano)}},
		{`INSERT INTO state VALUES('h','s','health',?)`, []any{state}},
		{`PRAGMA user_version=3`, nil},
	} {
		if _, err = db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Query(ctx, "h", "s", "metrics", time.Time{}, 1000)
	if err != nil || len(got) != 300 {
		t.Fatal(len(got), err)
	}
	for i, r := range got {
		want := expected[len(expected)-1-i]
		if r.ID != want.id || !r.ObservedAt.Equal(want.at) || !bytes.Equal(r.Data, want.data) {
			t.Fatal("migration changed observation", i)
		}
	}
	current, err := s.State(ctx, "h", "s", "health")
	if err != nil || string(current) != state {
		t.Fatal(string(current), err)
	}
	incidents, err := s.LatestEntities(ctx, "h", "incident")
	if err != nil || len(incidents) != 1 || incidents[0].ID != 901 {
		t.Fatal(incidents, err)
	}
	if inserted, err := s.AddOnce(ctx, "h", "s", "deployment", "deploy-key", at, map[string]string{"id": "deploy-a"}); err != nil || inserted {
		t.Fatal("dedup lost", inserted, err)
	}
	id, err := s.Add(ctx, "h", "s", "metrics", time.Now().UTC(), map[string]int{"next": 1})
	if err != nil || id != 903 {
		t.Fatal("sequence lost", id, err)
	}
}

func TestCompactConcurrentReplayAcrossDatabaseConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "history.db")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	ctx := context.Background()
	if err = a.Prune(ctx, config.History{}); err != nil {
		t.Fatal(err)
	}
	if err = b.Prune(ctx, config.History{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		at := time.Now().UTC()
		var wg sync.WaitGroup
		results := make(chan bool, 2)
		errors := make(chan error, 2)
		for _, s := range []*Store{a, b} {
			wg.Add(1)
			go func(s *Store) {
				defer wg.Done()
				inserted, err := s.AddUnique(ctx, "h", "", "host_metrics", at, map[string]int{"sample": i})
				results <- inserted
				errors <- err
			}(s)
		}
		wg.Wait()
		close(results)
		close(errors)
		count := 0
		for err := range errors {
			if err != nil {
				t.Fatal(err)
			}
		}
		for inserted := range results {
			if inserted {
				count++
			}
		}
		if count != 1 {
			t.Fatal("replay not atomic", count)
		}
	}
}

func TestCompactLargeRepeatedRecordsRespectDecodeBudget(t *testing.T) {
	s := auditStore(t)
	ctx := context.Background()
	at := time.Now().UTC()
	for i := 0; i < 100; i++ {
		if _, err := s.Add(ctx, "h", "s", "custom", at.Add(time.Duration(i)*time.Second), map[string]any{"large": strings.Repeat("x", 200000), "sequence": i}); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.Query(ctx, "h", "s", "custom", time.Time{}, 1000)
	if err != nil || len(records) != 100 {
		t.Fatal(len(records), err)
	}
}

// Opt-in because a real day's worth of writes is deliberately substantial.
// Both databases receive identical JSON, IDs and timestamps; no downsampling.
func TestFullDayLosslessStorageComparison(t *testing.T) {
	if os.Getenv("HOSTOPS_STORAGE_BENCH") != "1" {
		t.Skip("set HOSTOPS_STORAGE_BENCH=1 for the full-day storage fixture")
	}
	for _, changing := range []bool{false, true} {
		t.Run(fmt.Sprint(changing), func(t *testing.T) {
			s := auditStore(t)
			ctx := context.Background()
			if err := s.Prune(ctx, config.History{MetricDetail: "full"}); err != nil {
				t.Fatal(err)
			}
			legacy, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer legacy.Close()
			legacy.SetMaxOpenConns(1)
			for _, migration := range migrations[:3] {
				if _, err = legacy.Exec(migration); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Now().UTC().Add(-24 * time.Hour)
			var expected = map[string][]observation{}
			write := func(site, kind string, at time.Time, value any) {
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				id, err := s.Add(ctx, "h", site, kind, at, value)
				if err != nil {
					t.Fatal(err)
				}
				result, err := legacy.Exec(`INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES('h',?,?,?,?)`, site, kind, at.Format(timestampLayout), string(data))
				if err != nil {
					t.Fatal(err)
				}
				oldID, _ := result.LastInsertId()
				if oldID != id {
					t.Fatal("id changed", id, oldID)
				}
				key := site + "/" + kind
				expected[key] = append(expected[key], observation{id: id, at: at, data: data})
			}
			begin := time.Now()
			for cycle := 0; cycle < 2880; cycle++ {
				at := start.Add(time.Duration(cycle) * 30 * time.Second)
				if changing {
					at = at.Add(time.Duration((cycle*7919)%1_000_000_000) * time.Nanosecond)
				}
				for i := 0; i < 21; i++ {
					id := fmt.Sprintf("site-%d", i)
					v := 1.0
					memory := 100000000.0
					if changing {
						v = 5 + 4*math.Sin(float64(cycle+i)*0.17)
						memory += float64((cycle*7919+i*31)%5000) * 4096
					}
					sample := metrics.Sample{HostID: "h", SiteID: id, ObservedAt: at, Values: map[string]float64{"cpu_percent": v, "memory_used_bytes": memory}, Unavailable: []string{}}
					if changing {
						sample.Values["memory_limit_bytes"] = 512 * 1024 * 1024
						sample.Values["memory_percent"] = 100 * memory / (512 * 1024 * 1024)
						sample.Values["pids"] = float64(5 + (cycle+i)%4)
					}
					write(id, "metrics", at, sample)
					if cycle%2 == 0 {
						r := model.Report{HostID: "h", SiteID: id, Overall: "healthy", ObservedAt: at}
						for _, layer := range []string{"runtime", "proxy_route", "local_http", "dns", "tcp", "tls", "public_http"} {
							check := model.Check{HostID: "h", SiteID: id, Layer: layer, State: model.Pass, Summary: "Observed passing layer"}
							if changing && layer == "public_http" {
								check.LatencyMS = float64(100 + (cycle+i)%250)
								check.StatusCode = 200
								check.Summary = fmt.Sprintf("HTTP 200; %.0f ms", check.LatencyMS)
							}
							r.Checks = append(r.Checks, check)
						}
						if changing && cycle == 900 && i == 3 {
							r.Overall = "critical"
							r.Checks[6].State = model.Fail
						}
						write(id, "health", at, r)
					}
				}
				for _, offset := range []time.Duration{0, 15 * time.Second} {
					cpu, memory := 1.0, 38.0
					if changing {
						cpu = 10 + 9*math.Sin(float64(cycle)*0.31)
						memory += math.Sin(float64(cycle) * 0.01)
					}
					sample := metrics.Sample{HostID: "h", ObservedAt: at.Add(offset), Values: map[string]float64{"cpu_percent": cpu, "memory_percent": memory}, Unavailable: []string{}}
					if changing {
						for k, v := range map[string]float64{"memory_total_bytes": 4 << 30, "memory_used_bytes": memory * (4 << 30) / 100, "swap_used_bytes": 918 << 20, "uptime_seconds": float64(cycle)*30 + offset.Seconds(), "load_1": cpu / 100, "load_5": cpu / 150, "load_15": cpu / 200, "disk_total_bytes": 40 << 30, "disk_used_bytes": float64(35<<30) + float64(cycle)*4096, "disk_free_bytes": float64(5<<30) - float64(cycle)*4096, "disk_percent": 87.5 + float64(cycle)/100000, "disk_reserved_bytes": 0, "network_rx_bytes": float64(cycle) * 150000, "network_tx_bytes": float64(cycle) * 170000, "network_rx_bytes_per_second": vRate(cycle, 12000), "network_tx_bytes_per_second": vRate(cycle, 15000)} {
							sample.Values[k] = v
						}
					}
					write("", "host_metrics", at.Add(offset), sample)
				}
			}
			var oldSize, newSize, count, blocks int64
			if err = legacy.QueryRow(`SELECT page_count*page_size FROM pragma_page_count(),pragma_page_size()`).Scan(&oldSize); err != nil {
				t.Fatal(err)
			}
			if err = s.DB.QueryRow(`SELECT page_count*page_size FROM pragma_page_count(),pragma_page_size()`).Scan(&newSize); err != nil {
				t.Fatal(err)
			}
			if err = s.DB.QueryRow(`SELECT sum(samples),count(*) FROM blocks`).Scan(&count, &blocks); err != nil {
				t.Fatal(err)
			}
			t.Logf("24-hour fixture changing=%v: %d observations in %d blocks; old %.3f MiB, compact %.3f MiB; reduction %.2f%%; write comparison %s", changing, count, blocks, float64(oldSize)/(1<<20), float64(newSize)/(1<<20), 100*(1-float64(newSize)/float64(oldSize)), time.Since(begin))
			var activeBytes, sealedBytes int64
			s.DB.QueryRow(`SELECT COALESCE(sum(length(data)),0) FROM active_payloads`).Scan(&activeBytes)
			s.DB.QueryRow(`SELECT COALESCE(sum(length(data)),0) FROM sealed_payloads`).Scan(&sealedBytes)
			t.Logf("payloads: active %d bytes; sealed %d bytes", activeBytes, sealedBytes)
			for _, pageSize := range []int{512, 1024, 2048, 4096} {
				path := filepath.Join(t.TempDir(), "clone.db")
				if err := protectHistory(path, false); err != nil {
					t.Fatal(err)
				}
				if _, err = s.DB.Exec(`VACUUM INTO '` + strings.ReplaceAll(path, "'", "''") + `'`); err != nil {
					t.Fatal(err)
				}
				clone, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = clone.Exec(fmt.Sprintf("PRAGMA journal_mode=DELETE; PRAGMA page_size=%d; VACUUM", pageSize)); err != nil {
					clone.Close()
					t.Fatal(err)
				}
				var size int64
				if err = clone.QueryRow(`SELECT page_count*page_size FROM pragma_page_count(),pragma_page_size()`).Scan(&size); err != nil {
					clone.Close()
					t.Fatal(err)
				}
				clone.Close()
				t.Logf("compacted page size %d: %.3f MiB", pageSize, float64(size)/(1<<20))
			}
			fixtureLimit := int64(1024 * 1024)
			if changing {
				fixtureLimit = 3 * 1024 * 1024
			}
			if count != 96480 || newSize >= oldSize/20 || newSize > fixtureLimit {
				t.Fatal("size/count regression", count, oldSize, newSize)
			}
			// Decode all streams, including rows beyond the public 1,000-row limit.
			for key, want := range expected {
				sep := bytes.LastIndexByte([]byte(key), '/')
				site, kind := key[:sep], key[sep+1:]
				got, err := s.queryBlocks(ctx, "h", site, kind, time.Time{}, len(want)+1, true, false)
				if err != nil || len(got) != len(want) {
					t.Fatal(key, len(got), len(want), err)
				}
				sort.Slice(want, func(i, j int) bool { return want[i].id > want[j].id })
				for i := range got {
					if got[i].ID != want[i].id || !got[i].ObservedAt.Equal(want[i].at) || !bytes.Equal(got[i].Data, want[i].data) {
						t.Fatalf("%s observation %d changed", key, i)
					}
				}
			}
		})
	}
}

func vRate(cycle int, base float64) float64 { return base + base*0.4*math.Sin(float64(cycle)*0.23) }

func FuzzBlockDecoder(f *testing.F) {
	f.Add(encodeFrames([]observation{{id: 1, at: time.Now().UTC(), data: []byte(`{"value":12.5}`)}}), 1)
	f.Add([]byte{0}, 128)
	f.Fuzz(func(t *testing.T, data []byte, count int) {
		if len(data) > 64*1024 {
			return
		}
		_, _ = decodeFrames(data, count)
	})
}
