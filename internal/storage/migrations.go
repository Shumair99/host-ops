package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const schemaVersion = 5

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS records (id INTEGER PRIMARY KEY, host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL, observed_at TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS records_lookup ON records(host_id,kind,site_id,id DESC);
CREATE INDEX IF NOT EXISTS records_time ON records(observed_at);
CREATE TABLE IF NOT EXISTS dedup (host_id TEXT NOT NULL, kind TEXT NOT NULL, event_key TEXT NOT NULL, PRIMARY KEY(host_id,kind,event_key));
CREATE TABLE IF NOT EXISTS leases (host_id TEXT NOT NULL, kind TEXT NOT NULL, owner TEXT NOT NULL, expires_at INTEGER NOT NULL, PRIMARY KEY(host_id,kind));
CREATE TABLE IF NOT EXISTS state (host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL, data TEXT NOT NULL, PRIMARY KEY(host_id,site_id,kind));`,
	`CREATE INDEX IF NOT EXISTS records_history_dedup ON records(host_id,site_id,kind,observed_at);
CREATE TABLE IF NOT EXISTS history_imports (host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL, observed_at TEXT NOT NULL, PRIMARY KEY(host_id,site_id,kind,observed_at));`,
	`ALTER TABLE dedup ADD COLUMN observed_at TEXT NOT NULL DEFAULT '';
ALTER TABLE dedup ADD COLUMN record_id INTEGER;`,
	compactSchema,
	// Version 5 permits larger independent blocks and typed metric summaries.
	// Existing version 4 frames remain readable without rewriting their data.
	`DROP INDEX blocks_active;
CREATE UNIQUE INDEX blocks_active ON blocks(stream_id) WHERE encoding IN (0,2);`,
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return err
	}
	var initialPages int
	if err := db.QueryRow("PRAGMA page_count").Scan(&initialPages); err != nil {
		return err
	}
	if initialPages == 0 {
		if _, err := db.Exec("PRAGMA page_size=1024; PRAGMA auto_vacuum=FULL"); err != nil {
			return err
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	compacted := false
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return fmt.Errorf("history schema %d is newer than supported %d; upgrade HostOps", version, schemaVersion)
	}
	for version < schemaVersion {
		if _, err = tx.Exec(migrations[version]); err != nil {
			return fmt.Errorf("history migration %d: %w", version+1, err)
		}
		if version == 3 {
			if err = migrateCompact(context.Background(), tx); err != nil {
				return fmt.Errorf("history migration 4: %w", err)
			}
			compacted = true
		}
		version++
		if _, err = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE dedup SET observed_at=? WHERE observed_at=''", time.Now().UTC().Format(timestampLayout)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var vacuum, pageSize int
	if err = db.QueryRow("PRAGMA auto_vacuum").Scan(&vacuum); err != nil {
		return err
	}
	if err = db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	if vacuum != 1 || compacted || pageSize != 1024 {
		// Smaller pages avoid reserving 4 KiB for each ~1-2 KiB compressed
		// block. Page-size conversion requires rollback journal mode.
		if _, err = db.Exec("PRAGMA journal_mode=DELETE; PRAGMA page_size=1024; PRAGMA auto_vacuum=FULL; VACUUM"); err != nil {
			return fmt.Errorf("history compaction setup failed: %w", err)
		}
	}
	_, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=256; PRAGMA journal_size_limit=1048576")
	return err
}
