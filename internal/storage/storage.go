package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
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

// DefaultHistoryPath returns the private, per-user location used when a
// caller does not explicitly select a history database.
func DefaultHistoryPath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate the per-user history directory: %w", err)
	}
	if cacheDir == "" {
		return "", fmt.Errorf("the operating system did not provide a per-user cache directory")
	}
	return filepath.Join(cacheDir, "HostOps", "history.db"), nil
}

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
