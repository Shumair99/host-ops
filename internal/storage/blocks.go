package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const compactSchema = `
CREATE TABLE history_sequence (id INTEGER PRIMARY KEY CHECK(id=1), value INTEGER NOT NULL);
INSERT INTO history_sequence VALUES(1,0);
CREATE TABLE streams (id INTEGER PRIMARY KEY, host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL,
 latest_id INTEGER NOT NULL DEFAULT 0, latest_at TEXT NOT NULL DEFAULT '', latest_data BLOB,
 UNIQUE(host_id,site_id,kind));
CREATE TABLE blocks (id INTEGER PRIMARY KEY, stream_id INTEGER NOT NULL REFERENCES streams(id),
 first_id INTEGER NOT NULL, last_id INTEGER NOT NULL, first_at TEXT NOT NULL, last_at TEXT NOT NULL,
 samples INTEGER NOT NULL, encoding INTEGER NOT NULL,decoded_bytes INTEGER NOT NULL);
CREATE TABLE active_payloads (block_id INTEGER PRIMARY KEY REFERENCES blocks(id),data BLOB NOT NULL);
CREATE TABLE sealed_payloads (block_id INTEGER PRIMARY KEY REFERENCES blocks(id),data BLOB NOT NULL);
CREATE INDEX blocks_lookup ON blocks(stream_id,last_id DESC);
CREATE INDEX blocks_time ON blocks(stream_id,last_at);
CREATE UNIQUE INDEX blocks_active ON blocks(stream_id) WHERE encoding=0;
CREATE TABLE incident_heads (host_id TEXT NOT NULL, entity_id TEXT NOT NULL, record_id INTEGER NOT NULL, status TEXT NOT NULL,
 PRIMARY KEY(host_id,entity_id));
CREATE TABLE compact_state (host_id TEXT NOT NULL, site_id TEXT NOT NULL, kind TEXT NOT NULL, data BLOB NOT NULL, PRIMARY KEY(host_id,site_id,kind));
`

type block struct {
	id, streamID, firstID, lastID     int64
	host, site, kind, firstAt, lastAt string
	count, encoding                   int
	data                              []byte
}

func (b block) observations() ([]observation, error) {
	raw, err := unpackBlock(b.data, b.encoding)
	if err != nil {
		return nil, err
	}
	samples, err := decodeFrames(raw, b.count)
	if err != nil {
		return nil, err
	}
	if samples[0].id != b.firstID || samples[len(samples)-1].id != b.lastID {
		return nil, fmt.Errorf("history block id bounds mismatch")
	}
	for _, sample := range samples {
		at := sample.at.Format(timestampLayout)
		if at < b.firstAt || at > b.lastAt {
			return nil, fmt.Errorf("history block timestamp bounds mismatch")
		}
	}
	return samples, nil
}

func insertObservation(ctx context.Context, tx *sql.Tx, host, site, kind string, at time.Time, data []byte, existingID int64) (int64, error) {
	if host == "" || kind == "" {
		return 0, fmt.Errorf("record requires host_id and kind")
	}
	if at.Year() < 1 || at.Year() > 9999 {
		return 0, fmt.Errorf("history timestamp is outside RFC3339 range")
	}
	if len(data) > maxBlockBytes-128 {
		return 0, fmt.Errorf("history record exceeds %d-byte decode limit", maxBlockBytes-128)
	}
	if existingID == 0 && (kind == "metrics" || kind == "host_metrics") && at.Before(time.Now().Add(-24*time.Hour)) {
		for _, seconds := range []int{300, 1800} {
			_, _, index, err := summaryAt(ctx, tx, host, site, summaryName(kind, seconds), at.UTC().Truncate(time.Duration(seconds)*time.Second))
			if err != nil {
				return 0, err
			}
			if index >= 0 {
				return 0, fmt.Errorf("metric window already summarized; individual imports into closed windows are unsupported")
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO streams(host_id,site_id,kind) VALUES(?,?,?) ON CONFLICT(host_id,site_id,kind) DO NOTHING`, host, site, kind); err != nil {
		return 0, err
	}
	var streamID, previousID int64
	var previousAt string
	var previousData []byte
	if err := tx.QueryRowContext(ctx, `SELECT id,latest_id,latest_at,latest_data FROM streams WHERE host_id=? AND site_id=? AND kind=?`, host, site, kind).Scan(&streamID, &previousID, &previousAt, &previousData); err != nil {
		return 0, err
	}
	id := existingID
	if id == 0 {
		if err := tx.QueryRowContext(ctx, `UPDATE history_sequence SET value=value+1 WHERE id=1 RETURNING value`).Scan(&id); err != nil {
			return 0, err
		}
	}
	var b block
	var rawBytes, decodedBytes int
	err := tx.QueryRowContext(ctx, `SELECT b.id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,length(a.data),b.decoded_bytes FROM blocks b JOIN active_payloads a ON a.block_id=b.id WHERE b.stream_id=? AND b.encoding IN (0,2)`, streamID).Scan(&b.id, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &rawBytes, &decodedBytes)
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	previous := observation{at: time.Unix(0, 0)}
	if err == nil {
		if b.encoding == 2 {
			if err = loadPayload(ctx, tx, &b); err != nil {
				return 0, err
			}
			raw, decodeErr := unpackBlock(b.data, 2)
			if decodeErr != nil {
				return 0, decodeErr
			}
			rawBytes = len(raw)
			if _, err = tx.ExecContext(ctx, `UPDATE active_payloads SET data=? WHERE block_id=?`, raw, b.id); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE blocks SET encoding=0 WHERE id=?`, b.id); err != nil {
				return 0, err
			}
			b.encoding = 0
		}
		previousData, err = unpackBlock(previousData, 1)
		if err != nil {
			return 0, err
		}
		previousAtTime, parseErr := time.Parse(time.RFC3339Nano, previousAt)
		if parseErr != nil {
			return 0, parseErr
		}
		previous = observation{id: previousID, at: previousAtTime, data: previousData}
	}
	sample := observation{id: id, at: at.UTC(), data: data}
	frame := appendFrame(nil, previous, sample)
	// Seal before a large append, so the decoder's bound applies to every block.
	if b.id != 0 && (rawBytes+len(frame) > blockTargetBytes || decodedBytes+len(data) > maxBlockBytes) {
		if err = sealBlock(ctx, tx, b.id); err != nil {
			return 0, err
		}
		b.id, b.count = 0, 0
		frame = appendFrame(nil, observation{at: time.Unix(0, 0)}, sample)
	}
	timestamp := at.UTC().Format(timestampLayout)
	if b.id == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO blocks(stream_id,first_id,last_id,first_at,last_at,samples,encoding,decoded_bytes) VALUES(?,?,?,?,?,1,0,?)`, streamID, id, id, timestamp, timestamp, len(data))
		if err != nil {
			return 0, err
		}
		b.id, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
		b.count = 1
		if _, err = tx.ExecContext(ctx, `INSERT INTO active_payloads(block_id,data) VALUES(?,?)`, b.id, frame); err != nil {
			return 0, err
		}
	} else {
		if timestamp < b.firstAt {
			b.firstAt = timestamp
		}
		if timestamp > b.lastAt {
			b.lastAt = timestamp
		}
		if _, err := tx.ExecContext(ctx, `UPDATE blocks SET last_id=?,first_at=?,last_at=?,samples=samples+1,decoded_bytes=decoded_bytes+? WHERE id=?`, id, b.firstAt, b.lastAt, len(data), b.id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE active_payloads SET data=CAST(data || ? AS BLOB) WHERE block_id=?`, frame, b.id); err != nil {
			return 0, err
		}
		b.count++
	}
	if b.count >= blockSamples || len(frame) >= blockTargetBytes {
		if err := sealBlock(ctx, tx, b.id); err != nil {
			return 0, err
		}
	}
	packed, err := packBlock(data)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE streams SET latest_id=?,latest_at=?,latest_data=? WHERE id=?`, id, timestamp, packed, streamID); err != nil {
		return 0, err
	}
	if kind == "incident" {
		var entity struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(data, &entity); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO incident_heads(host_id,entity_id,record_id,status) VALUES(?,?,?,?) ON CONFLICT(host_id,entity_id) DO UPDATE SET record_id=excluded.record_id,status=excluded.status`, host, entity.ID, id, entity.Status); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func sealBlock(ctx context.Context, tx *sql.Tx, id int64) error {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT data FROM active_payloads WHERE block_id=?`, id).Scan(&raw); err != nil {
		return err
	}
	data, err := packSealedBlock(raw)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sealed_payloads(block_id,data) VALUES(?,?)`, id, data); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM active_payloads WHERE block_id=?`, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE blocks SET encoding=1 WHERE id=?`, id)
	return err
}

// queryBlocks narrows by stream/time before decompressing. ID bounds let a
// limited query stop without reading the rest of a multi-day history.
func (s *Store) queryBlocks(ctx context.Context, host, site, kind string, since time.Time, limit int, exactSite, oldest bool) ([]Record, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return queryBlocksTx(ctx, tx, host, site, kind, since, limit, exactSite, oldest, false)
}

func queryBlocksTx(ctx context.Context, tx *sql.Tx, host, site, kind string, since time.Time, limit int, exactSite, oldest, byTime bool) ([]Record, error) {
	order := "b.last_id DESC"
	if oldest {
		order = "b.first_at,b.first_id"
	} else if byTime {
		order = "b.last_at DESC,b.last_id DESC"
	}
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,s.host_id,s.site_id,s.kind
 FROM blocks b JOIN streams s ON s.id=b.stream_id WHERE s.host_id=? AND (?=0 OR s.site_id=?) AND (?='' OR s.kind=?) AND b.last_at>=? ORDER BY `+order,
		host, exactSite || site != "", site, kind, kind, since.UTC().Format(timestampLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Record{}
	for rows.Next() {
		var b block
		if err = rows.Scan(&b.id, &b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &b.host, &b.site, &b.kind); err != nil {
			rows.Close()
			return nil, err
		}
		if len(result) >= limit && ((!oldest && !byTime && b.lastID < result[limit-1].ID) || (!oldest && byTime && b.lastAt < result[limit-1].ObservedAt.UTC().Format(timestampLayout)) || (oldest && b.firstAt > result[limit-1].ObservedAt.UTC().Format(timestampLayout))) {
			break
		}
		if err = loadPayload(ctx, tx, &b); err != nil {
			return nil, err
		}
		observed, err := b.observations()
		if err != nil {
			return nil, fmt.Errorf("read history block %d: %w", b.id, err)
		}
		for _, sample := range observed {
			if !sample.at.Before(since) {
				result = append(result, Record{ID: sample.id, HostID: b.host, SiteID: b.site, Kind: b.kind, ObservedAt: sample.at, Data: sample.data})
			}
		}
		sort.Slice(result, func(i, j int) bool {
			if byTime && !result[i].ObservedAt.Equal(result[j].ObservedAt) {
				return result[i].ObservedAt.After(result[j].ObservedAt)
			}
			if !oldest {
				return result[i].ID > result[j].ID
			}
			if result[i].ObservedAt.Equal(result[j].ObservedAt) {
				return result[i].ID < result[j].ID
			}
			return result[i].ObservedAt.Before(result[j].ObservedAt)
		})
		if len(result) > limit {
			result = result[:limit]
		}
	}
	return result, rows.Err()
}

func observationExists(ctx context.Context, tx *sql.Tx, host, site, kind string, at time.Time) (bool, error) {
	timestamp := at.UTC().Format(timestampLayout)
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,COALESCE(a.data,c.data) FROM blocks b JOIN streams s ON b.stream_id=s.id LEFT JOIN active_payloads a ON a.block_id=b.id LEFT JOIN sealed_payloads c ON c.block_id=b.id WHERE s.host_id=? AND s.site_id=? AND s.kind=? AND b.first_at<=? AND b.last_at>=?`, host, site, kind, timestamp, timestamp)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var b block
		if err = rows.Scan(&b.id, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &b.data); err != nil {
			return false, err
		}
		observed, err := b.observations()
		if err != nil {
			return false, err
		}
		for _, sample := range observed {
			if sample.at.Equal(at) {
				return true, nil
			}
		}
	}
	return false, rows.Err()
}

func loadPayload(ctx context.Context, tx *sql.Tx, b *block) error {
	table := "active_payloads"
	if b.encoding == 1 {
		table = "sealed_payloads"
	}
	return tx.QueryRowContext(ctx, `SELECT data FROM `+table+` WHERE block_id=?`, b.id).Scan(&b.data)
}

func putState(ctx context.Context, tx *sql.Tx, host, site, kind string, data []byte) error {
	if len(data) > maxBlockBytes {
		return fmt.Errorf("current state exceeds decode limit")
	}
	packed, err := packBlock(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO state(host_id,site_id,kind,data) VALUES(?,?,?,?) ON CONFLICT(host_id,site_id,kind) DO UPDATE SET data=excluded.data`, host, site, kind, packed)
	return err
}

func migrateCompact(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,host_id,site_id,kind,observed_at,data FROM records ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var host, site, kind, timestamp string
		var data []byte
		if err = rows.Scan(&id, &host, &site, &kind, &timestamp, &data); err != nil {
			rows.Close()
			return err
		}
		if id <= 0 {
			rows.Close()
			return fmt.Errorf("legacy history record has unsupported nonpositive id %d", id)
		}
		at, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			rows.Close()
			return err
		}
		if !json.Valid(data) {
			rows.Close()
			return fmt.Errorf("invalid JSON in legacy history record %d", id)
		}
		if _, err = insertObservation(ctx, tx, host, site, kind, at, data, id); err != nil {
			rows.Close()
			return err
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if _, err = tx.ExecContext(ctx, `UPDATE history_sequence SET value=COALESCE((SELECT max(latest_id) FROM streams),0) WHERE id=1`); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT host_id,site_id,kind,data FROM state`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var host, site, kind string
		var data []byte
		if err = rows.Scan(&host, &site, &kind, &data); err != nil {
			rows.Close()
			return err
		}
		if len(data) > maxBlockBytes {
			rows.Close()
			return fmt.Errorf("legacy current state exceeds decode limit")
		}
		packed, err := packBlock(data)
		if err != nil {
			rows.Close()
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO compact_state(host_id,site_id,kind,data) VALUES(?,?,?,?)`, host, site, kind, packed); err != nil {
			rows.Close()
			return err
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	_, err = tx.ExecContext(ctx, `DELETE FROM dedup WHERE record_id IS NOT NULL AND record_id NOT IN (SELECT id FROM records); DROP TABLE history_imports; DROP TABLE records; DROP TABLE state; ALTER TABLE compact_state RENAME TO state`)
	return err
}
