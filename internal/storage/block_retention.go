package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
)

func (s *Store) retentionBlocks(ctx context.Context, pressure bool, policy config.History) ([]block, error) {
	where := ""
	args := []any{}
	order := "b.first_at,b.first_id"
	if !pressure {
		now := time.Now()
		where = `WHERE (s.kind IN ('metrics','host_metrics') AND b.first_at<?) OR (s.kind IN ('health','external_health','snapshot','inventory') AND b.first_at<?) OR (s.kind IN ('event','change','deployment','incident','alert','backup','restore') AND b.first_at<?)`
		for _, days := range []int{policy.MetricsDays, policy.RetentionDays, policy.EvidenceDays} {
			args = append(args, now.Add(-time.Duration(days)*24*time.Hour).UTC().Format(timestampLayout))
		}
		where += ` OR (s.kind IN ('metrics_5m','host_metrics_5m') AND b.first_at<?) OR (s.kind IN ('metrics_30m','host_metrics_30m') AND b.first_at<?)`
		minuteDays := min(7, policy.MetricsDays)
		for _, days := range []int{minuteDays, policy.MetricsDays} {
			args = append(args, now.Add(-time.Duration(days)*24*time.Hour).UTC().Truncate(30*time.Minute).Format(timestampLayout))
		}
	} else {
		order = `CASE WHEN s.kind IN ('metrics','host_metrics','metrics_5m','host_metrics_5m','metrics_30m','host_metrics_30m','health','external_health','inventory','snapshot') THEN 0 ELSE 1 END,b.first_at,b.first_id`
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,s.kind FROM blocks b JOIN streams s ON s.id=b.stream_id `+where+` ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []block{}
	for rows.Next() {
		var b block
		if err = rows.Scan(&b.id, &b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &b.kind); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func retentionCutoff(kind string, policy config.History) time.Time {
	days := policy.EvidenceDays
	switch kind {
	case "metrics", "host_metrics", "metrics_30m", "host_metrics_30m":
		days = policy.MetricsDays
	case "metrics_5m", "host_metrics_5m":
		days = min(7, policy.MetricsDays)
	case "health", "external_health", "snapshot", "inventory":
		days = policy.RetentionDays
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if _, _, summary := summaryKind(kind); summary {
		cutoff = cutoff.UTC().Truncate(30 * time.Minute)
	}
	return cutoff
}

func pruneBlock(ctx context.Context, tx *sql.Tx, b block, cutoff time.Time) (int64, error) {
	// Candidate selection precedes writer reservation. A concurrent append can
	// grow/seal that block in between, so read its current descriptor under the
	// write transaction before decoding or rebuilding its payload.
	err := tx.QueryRowContext(ctx, `SELECT stream_id,first_id,last_id,first_at,last_at,samples,encoding FROM blocks WHERE id=?`, b.id).Scan(&b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var protected int64
	if err := tx.QueryRowContext(ctx, `SELECT latest_id FROM streams WHERE id=?`, b.streamID).Scan(&protected); err != nil {
		return 0, err
	}
	if _, _, summary := summaryKind(b.kind); summary {
		protected = 0
	}
	heads := map[int64]bool{protected: true}
	rows, err := tx.QueryContext(ctx, `SELECT record_id FROM incident_heads WHERE status='open' AND record_id>=? AND record_id<=?`, b.firstID, b.lastID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		heads[id] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if err = loadPayload(ctx, tx, &b); err != nil {
		return 0, err
	}
	observed, err := b.observations()
	if err != nil {
		return 0, err
	}
	retained := make([]observation, 0, len(observed))
	removed := []any{}
	for _, sample := range observed {
		if !heads[sample.id] && (cutoff.IsZero() || sample.at.Before(cutoff)) {
			removed = append(removed, sample.id)
		} else {
			retained = append(retained, sample)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(removed)), ",")
	if _, err = tx.ExecContext(ctx, `DELETE FROM dedup WHERE record_id IN (`+placeholders+`)`, removed...); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM incident_heads WHERE record_id IN (`+placeholders+`)`, removed...); err != nil {
		return 0, err
	}
	err = replaceBlock(ctx, tx, b, retained)
	return int64(len(removed)), err
}

func replaceBlock(ctx context.Context, tx *sql.Tx, b block, retained []observation) error {
	var err error
	if len(retained) == 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM active_payloads WHERE block_id=?`, b.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM sealed_payloads WHERE block_id=?`, b.id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM blocks WHERE id=?`, b.id)
	} else {
		decodedBytes := 0
		firstAt, lastAt := retained[0].at.UTC().Format(timestampLayout), retained[0].at.UTC().Format(timestampLayout)
		for _, sample := range retained {
			decodedBytes += len(sample.data)
			at := sample.at.UTC().Format(timestampLayout)
			if at < firstAt {
				firstAt = at
			}
			if at > lastAt {
				lastAt = at
			}
		}
		if decodedBytes > maxBlockBytes {
			return fmt.Errorf("rebuilt history block exceeds decode limit")
		}
		data := encodeFrames(retained)
		if b.encoding == 1 {
			data, err = packSealedBlock(data)
			if err != nil {
				return err
			}
		}
		table := "active_payloads"
		if b.encoding == 1 {
			table = "sealed_payloads"
		}
		if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET data=? WHERE block_id=?`, data, b.id); err != nil {
			return err
		}
		encoding := b.encoding
		if encoding == 2 {
			encoding = 0
		}
		_, err = tx.ExecContext(ctx, `UPDATE blocks SET first_id=?,last_id=?,first_at=?,last_at=?,samples=?,decoded_bytes=?,encoding=? WHERE id=?`, retained[0].id, retained[len(retained)-1].id, firstAt, lastAt, len(retained), decodedBytes, encoding, b.id)
		if err == nil && b.encoding != 1 {
			// An expired out-of-order summary can be the insertion head while
			// younger rows survive. The next frame must delta from the surviving
			// active tail, not from that removed or modified head.
			last := retained[len(retained)-1]
			var packed []byte
			packed, err = packBlock(last.data)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE streams SET latest_id=?,latest_at=?,latest_data=? WHERE id=?`, last.id, last.at.UTC().Format(timestampLayout), packed, b.streamID)
			}
		}
	}
	return err
}

func (s *Store) prune(ctx context.Context, policy config.History) error {
	policy = policy.Defaults()
	blocks, err := s.retentionBlocks(ctx, false, policy)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE history_sequence SET value=value WHERE id=1`); err != nil {
		return err
	}
	if err = rollupMetrics(ctx, tx, policy, time.Now()); err != nil {
		return err
	}
	for _, b := range blocks {
		if _, err = pruneBlock(ctx, tx, b, retentionCutoff(b.kind, policy)); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM dedup WHERE record_id IS NULL AND observed_at<?`, time.Now().Add(-time.Duration(policy.EvidenceDays)*24*time.Hour).UTC().Format(timestampLayout)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM leases WHERE expires_at<?`, time.Now().UnixNano()); err != nil {
		return err
	}
	if err = compactMetricActives(ctx, tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var pages, pageSize int64
	if err = s.DB.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return err
	}
	limit := int64(policy.MaxDBMB) * 1024 * 1024 / pageSize
	if err = s.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return err
	}
	pruned := int64(0)
	if pages >= limit*85/100 {
		blocks, err = s.retentionBlocks(ctx, true, policy)
		if err != nil {
			return err
		}
		for _, b := range blocks {
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE history_sequence SET value=value WHERE id=1`); err != nil {
				tx.Rollback()
				return err
			}
			count, pruneErr := pruneBlock(ctx, tx, b, time.Time{})
			if pruneErr != nil {
				tx.Rollback()
				return pruneErr
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			pruned += count
			if err = s.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
				return err
			}
			if pages < limit*85/100 {
				break
			}
		}
		if pages >= limit*85/100 {
			return fmt.Errorf("history storage budget exhausted by protected current evidence; increase history.max_db_mb")
		}
	}
	var actualLimit int64
	if err = s.DB.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", limit)).Scan(&actualLimit); err != nil {
		return err
	}
	if actualLimit > limit {
		return fmt.Errorf("history file exceeds configured storage budget")
	}
	if pruned > 0 {
		data, _ := json.Marshal(map[string]any{"observed_at": time.Now().UTC(), "pruned_records": pruned, "max_db_mb": policy.MaxDBMB, "issue": "Storage budget reached; oldest history was pruned"})
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err = putState(ctx, tx, "hostops", "", "history_pressure", data); err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	_, err = s.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}
