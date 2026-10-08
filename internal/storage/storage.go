package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct {
	DB            *sql.DB
	stateMu       sync.Mutex
	locks         map[string]*sync.Mutex
	maintenanceMu sync.Mutex
	policy        config.History
	maintainedAt  time.Time
}

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

type Record struct {
	ID         int64           `json:"id"`
	HostID     string          `json:"host_id"`
	SiteID     string          `json:"site_id"`
	Kind       string          `json:"kind"`
	ObservedAt time.Time       `json:"observed_at"`
	Data       json.RawMessage `json:"data"`
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		_, dirErr := os.Stat(filepath.Dir(path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		if err := protectHistory(path, os.IsNotExist(dirErr)); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		f.Close()
		if err := os.Chmod(path, 0600); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	err = migrate(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	if path != ":memory:" {
		if err = protectHistory(path, false); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Store{DB: db, policy: (config.History{}).Defaults()}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) LockState(key string) func() {
	s.stateMu.Lock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	lock := s.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[key] = lock
	}
	s.stateMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (s *Store) Add(ctx context.Context, host, site, kind string, at time.Time, value any) (int64, error) {
	if err := s.beforeWrite(ctx); err != nil {
		return 0, err
	}
	if host == "" || kind == "" {
		return 0, fmt.Errorf("record requires host_id and kind")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	result, err := s.DB.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, site, kind, at.UTC().Format(timestampLayout), string(data))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// AddUnique inserts a sample once per host/site/kind/observation timestamp.
// Persistent VPS history is replayed after reconnects, so timestamp-based
// deduplication keeps those replays from multiplying local history rows.
func (s *Store) AddUnique(ctx context.Context, host, site, kind string, at time.Time, value any) (bool, error) {
	if err := s.beforeWrite(ctx); err != nil {
		return false, err
	}
	if host == "" || kind == "" {
		return false, fmt.Errorf("record requires host_id and kind")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	timestamp := at.UTC().Format(timestampLayout)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO history_imports(host_id,site_id,kind,observed_at) VALUES(?,?,?,?)`, host, site, kind, timestamp)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows == 0 {
		return false, err
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM records WHERE host_id=? AND site_id=? AND kind=? AND observed_at=? LIMIT 1`, host, site, kind, timestamp).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)`, host, site, kind, timestamp, string(data)); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return err == nil && exists == 0, nil
}

func (s *Store) Reports(ctx context.Context, host string, reports []model.Report) error {
	return s.saveReports(ctx, host, reports, true)
}

func (s *Store) UpdateReports(ctx context.Context, host string, reports []model.Report) error {
	return s.saveReports(ctx, host, reports, false)
}

func (s *Store) saveReports(ctx context.Context, host string, reports []model.Report, history bool) error {
	if err := s.beforeWrite(ctx); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range reports {
		if r.HostID != host {
			return fmt.Errorf("report host mismatch")
		}
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO state(host_id,site_id,kind,data) VALUES(?,?,'health',?) ON CONFLICT(host_id,site_id,kind) DO UPDATE SET data=excluded.data", host, r.SiteID, string(data)); err != nil {
			return err
		}
		var previousData string
		err = tx.QueryRowContext(ctx, "SELECT data FROM records WHERE host_id=? AND site_id=? AND kind='health' ORDER BY id DESC LIMIT 1", host, r.SiteID).Scan(&previousData)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var previous model.Report
		if err == nil && json.Unmarshal([]byte(previousData), &previous) == nil && sameHealth(previous, r) && (!history || r.ObservedAt.Sub(previous.ObservedAt) < time.Minute) {
			continue
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, r.SiteID, "health", r.ObservedAt.UTC().Format(timestampLayout), string(data)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Latest(ctx context.Context, host string) ([]model.Report, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM (SELECT data FROM state WHERE host_id=? AND kind='health' UNION ALL SELECT data FROM records WHERE id IN (SELECT max(id) FROM records WHERE host_id=? AND kind='health' GROUP BY site_id) AND site_id NOT IN (SELECT site_id FROM state WHERE host_id=? AND kind='health')) ORDER BY json_extract(data,'$.site_id')`, host, host, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := []model.Report{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		var r model.Report
		if err = json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

func (s *Store) Query(ctx context.Context, host, site, kind string, since time.Time, limit int) ([]Record, error) {
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,host_id,site_id,kind,observed_at,data FROM records WHERE host_id=? AND (?='' OR site_id=?) AND (?='' OR kind=?) AND observed_at>=? ORDER BY id DESC LIMIT ?`, host, site, site, kind, kind, since.UTC().Format(timestampLayout), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []Record{}
	for rows.Next() {
		var r Record
		var at, data string
		if err = rows.Scan(&r.ID, &r.HostID, &r.SiteID, &r.Kind, &at, &data); err != nil {
			return nil, err
		}
		r.ObservedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		r.Data = json.RawMessage(data)
		records = append(records, r)
	}
	return records, rows.Err()
}

// Oldest supplies the first observation in a window without discarding it when
// a bounded recent-sample query has more than its row limit.
func (s *Store) Oldest(ctx context.Context, host, site, kind string, since time.Time) (*Record, error) {
	var record Record
	var at, data string
	err := s.DB.QueryRowContext(ctx, `SELECT id,host_id,site_id,kind,observed_at,data FROM records WHERE host_id=? AND site_id=? AND kind=? AND observed_at>=? ORDER BY observed_at,id LIMIT 1`, host, site, kind, since.UTC().Format(timestampLayout)).Scan(&record.ID, &record.HostID, &record.SiteID, &record.Kind, &at, &data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record.ObservedAt, err = time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return nil, err
	}
	record.Data = json.RawMessage(data)
	return &record, nil
}

// LatestEntities retains open incidents regardless of how long ago they began.
func (s *Store) LatestEntities(ctx context.Context, host, kind string) ([]Record, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,host_id,site_id,kind,observed_at,data FROM records WHERE id IN (SELECT max(id) FROM records WHERE host_id=? AND kind=? GROUP BY json_extract(data,'$.id')) ORDER BY id DESC LIMIT 1000`, host, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []Record{}
	for rows.Next() {
		var r Record
		var at, data string
		if err = rows.Scan(&r.ID, &r.HostID, &r.SiteID, &r.Kind, &at, &data); err != nil {
			return nil, err
		}
		r.ObservedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		r.Data = json.RawMessage(data)
		records = append(records, r)
	}
	return records, rows.Err()
}

func (s *Store) AddOnce(ctx context.Context, host, site, kind, key string, at time.Time, value any) (bool, error) {
	if err := s.beforeWrite(ctx); err != nil {
		return false, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO dedup(host_id,kind,event_key,observed_at) VALUES(?,?,?,?)", host, kind, key, at.UTC().Format(timestampLayout))
	if err != nil {
		return false, err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return false, nil
	}
	result, err = tx.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, site, kind, at.UTC().Format(timestampLayout), string(data))
	if err != nil {
		return false, err
	}
	recordID, err := result.LastInsertId()
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE dedup SET record_id=? WHERE host_id=? AND kind=? AND event_key=?", recordID, host, kind, key); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) State(ctx context.Context, host, site, kind string) (json.RawMessage, error) {
	var data string
	err := s.DB.QueryRowContext(ctx, "SELECT data FROM state WHERE host_id=? AND site_id=? AND kind=?", host, site, kind).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return json.RawMessage(data), err
}

func (s *Store) Transition(ctx context.Context, host, site, stateKind string, state any, eventKind string, at time.Time, event any) error {
	if err := s.beforeWrite(ctx); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	eventData, err := json.Marshal(event)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO state(host_id,site_id,kind,data) VALUES(?,?,?,?) ON CONFLICT(host_id,site_id,kind) DO UPDATE SET data=excluded.data", host, site, stateKind, string(data)); err != nil {
		return err
	}
	if string(eventData) != "null" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, site, eventKind, at.UTC().Format(timestampLayout), string(eventData)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
