package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }
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
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
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
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS records (id INTEGER PRIMARY KEY, host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL, observed_at TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS records_lookup ON records(host_id,kind,site_id,id DESC);
CREATE INDEX IF NOT EXISTS records_time ON records(observed_at); PRAGMA user_version=1;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) Add(ctx context.Context, host, site, kind string, at time.Time, value any) (int64, error) {
	if host == "" || kind == "" {
		return 0, fmt.Errorf("record requires host_id and kind")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	result, err := s.DB.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, site, kind, at.UTC().Format(time.RFC3339Nano), string(data))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) Reports(ctx context.Context, host string, reports []model.Report) error {
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
		if _, err = tx.ExecContext(ctx, "INSERT INTO records(host_id,site_id,kind,observed_at,data) VALUES(?,?,?,?,?)", host, r.SiteID, "health", r.ObservedAt.UTC().Format(time.RFC3339Nano), string(data)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Latest(ctx context.Context, host string) ([]model.Report, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data FROM records WHERE id IN (SELECT max(id) FROM records WHERE host_id=? AND kind='health' GROUP BY site_id) ORDER BY site_id`, host)
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
	rows, err := s.DB.QueryContext(ctx, `SELECT id,host_id,site_id,kind,observed_at,data FROM records WHERE host_id=? AND (?='' OR site_id=?) AND (?='' OR kind=?) AND observed_at>=? ORDER BY id DESC LIMIT ?`, host, site, site, kind, kind, since.UTC().Format(time.RFC3339Nano), limit)
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
