package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

// preserve protects the last view of each stream and every current open incident.
const preserve = `id NOT IN (SELECT max(id) FROM records GROUP BY host_id,site_id,kind)
AND id NOT IN (SELECT id FROM records WHERE kind='incident' AND json_extract(data,'$.status')='open'
AND id IN (SELECT max(id) FROM records WHERE kind='incident' GROUP BY host_id,json_extract(data,'$.id')))`

var httpLatencySummary = regexp.MustCompile(`^(HTTP [0-9]{3}); [0-9]+ ms(.*)$`)

func sameHealth(a, b model.Report) bool {
	a.ObservedAt, b.ObservedAt = time.Time{}, time.Time{}
	a.Checks = append([]model.Check(nil), a.Checks...)
	b.Checks = append([]model.Check(nil), b.Checks...)
	for i := range a.Checks {
		a.Checks[i].LatencyMS = 0
		if a.Checks[i].Layer == "public_http" || a.Checks[i].Layer == "local_http" {
			a.Checks[i].Summary = httpLatencySummary.ReplaceAllString(a.Checks[i].Summary, "$1$2")
		}
	}
	for i := range b.Checks {
		b.Checks[i].LatencyMS = 0
		if b.Checks[i].Layer == "public_http" || b.Checks[i].Layer == "local_http" {
			b.Checks[i].Summary = httpLatencySummary.ReplaceAllString(b.Checks[i].Summary, "$1$2")
		}
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func (s *Store) beforeWrite(ctx context.Context) error {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if time.Since(s.maintainedAt) < time.Minute {
		var size int64
		if err := s.DB.QueryRowContext(ctx, `SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&size); err != nil {
			return err
		}
		if size < int64(s.policy.MaxDBMB)*1024*1024*85/100 {
			return nil
		}
	}
	if err := s.prune(ctx, s.policy); err != nil {
		return err
	}
	s.maintainedAt = time.Now()
	return nil
}

func (s *Store) Prune(ctx context.Context, policy config.History) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	s.policy = policy.Defaults()
	if err := s.prune(ctx, s.policy); err != nil {
		return err
	}
	s.maintainedAt = time.Now()
	return nil
}

func (s *Store) prune(ctx context.Context, policy config.History) error {
	policy = policy.Defaults()
	now := time.Now()
	for _, group := range []struct {
		kinds string
		days  int
	}{
		{"'metrics','host_metrics'", policy.MetricsDays},
		{"'health','external_health','snapshot','inventory'", policy.RetentionDays},
		{"'event','change','deployment','incident','alert','backup','restore'", policy.EvidenceDays},
	} {
		cutoff := now.Add(-time.Duration(group.days) * 24 * time.Hour).UTC().Format(timestampLayout)
		if _, err := s.DB.ExecContext(ctx, "DELETE FROM records WHERE kind IN ("+group.kinds+") AND observed_at<? AND "+preserve, cutoff); err != nil {
			return err
		}
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM history_imports WHERE NOT EXISTS (SELECT 1 FROM records WHERE records.host_id=history_imports.host_id AND records.site_id=history_imports.site_id AND records.kind=history_imports.kind AND records.observed_at=history_imports.observed_at)`); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM dedup WHERE (record_id IS NOT NULL AND record_id NOT IN (SELECT id FROM records)) OR (record_id IS NULL AND observed_at<?)", now.Add(-time.Duration(policy.EvidenceDays)*24*time.Hour).UTC().Format(timestampLayout)); err != nil {
		return err
	}
	if _, err := s.DB.ExecContext(ctx, "DELETE FROM leases WHERE expires_at<?", now.UnixNano()); err != nil {
		return err
	}
	var pages, pageSize int64
	if err := s.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return err
	}
	limit := int64(policy.MaxDBMB) * 1024 * 1024 / pageSize
	pruned := int64(0)
	for {
		if err := s.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
			return err
		}
		if pages < limit*85/100 {
			break
		}
		result, err := s.DB.ExecContext(ctx, `DELETE FROM records WHERE id IN (SELECT id FROM records WHERE `+preserve+` ORDER BY CASE WHEN kind IN ('metrics','host_metrics','health','external_health','inventory','snapshot') THEN 0 ELSE 1 END,observed_at,id LIMIT 500)`)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		pruned += count
		if count == 0 {
			return fmt.Errorf("history storage budget exhausted by protected current evidence; increase history.max_db_mb")
		}
	}
	var actualLimit int64
	if err := s.DB.QueryRowContext(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", limit)).Scan(&actualLimit); err != nil {
		return err
	}
	if actualLimit > limit {
		return fmt.Errorf("history file exceeds configured storage budget")
	}
	if pruned > 0 {
		data, _ := json.Marshal(map[string]any{"observed_at": now.UTC(), "pruned_records": pruned, "max_db_mb": policy.MaxDBMB, "issue": "Storage budget reached; oldest history was pruned"})
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO state(host_id,site_id,kind,data) VALUES('hostops','','history_pressure',?) ON CONFLICT(host_id,site_id,kind) DO UPDATE SET data=excluded.data`, string(data)); err != nil {
			return err
		}
	}
	// Unlinked import keys are removed after pressure pruning as well.
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM history_imports WHERE NOT EXISTS (SELECT 1 FROM records WHERE records.host_id=history_imports.host_id AND records.site_id=history_imports.site_id AND records.kind=history_imports.kind AND records.observed_at=history_imports.observed_at); DELETE FROM dedup WHERE record_id IS NOT NULL AND record_id NOT IN (SELECT id FROM records)`); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
