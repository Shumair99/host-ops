package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
)

func (s *Store) Add(ctx context.Context, host, site, kind string, at time.Time, value any) (int64, error) {
	if err := s.beforeWrite(ctx); err != nil {
		return 0, err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertObservation(ctx, tx, host, site, kind, at, data, 0)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// The retained observations themselves are the replay index; a second key row
// per sample would both duplicate timestamps and grow the database needlessly.
func (s *Store) AddUnique(ctx context.Context, host, site, kind string, at time.Time, value any) (bool, error) {
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
	if host == "" || kind == "" {
		return false, fmt.Errorf("record requires host_id and kind")
	}
	// Reserve the writer before reading the replay index; independent clients
	// must not race a read transaction's upgrade to a writer.
	if _, err = tx.ExecContext(ctx, `UPDATE history_sequence SET value=value WHERE id=1`); err != nil {
		return false, err
	}
	exists, err := observationExists(ctx, tx, host, site, kind, at)
	if err != nil || exists {
		return false, err
	}
	if _, err = insertObservation(ctx, tx, host, site, kind, at, data, 0); err != nil {
		return false, err
	}
	return true, tx.Commit()
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
		if err = putState(ctx, tx, host, r.SiteID, "health", data); err != nil {
			return err
		}
		var previousData []byte
		err = tx.QueryRowContext(ctx, `SELECT latest_data FROM streams WHERE host_id=? AND site_id=? AND kind='health'`, host, r.SiteID).Scan(&previousData)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		var previous model.Report
		if err == nil {
			previousData, err = unpackBlock(previousData, 1)
			if err != nil {
				return err
			}
		}
		if err == nil && json.Unmarshal(previousData, &previous) == nil && sameHealth(previous, r) && (!history || r.ObservedAt.Sub(previous.ObservedAt) < time.Minute) {
			continue
		}
		if _, err = insertObservation(ctx, tx, host, r.SiteID, "health", r.ObservedAt, data, 0); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Latest(ctx context.Context, host string) ([]model.Report, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT data,1 FROM state WHERE host_id=? AND kind='health'
 UNION ALL SELECT latest_data,1 FROM streams WHERE host_id=? AND kind='health' AND site_id NOT IN (SELECT site_id FROM state WHERE host_id=? AND kind='health')`, host, host, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reports := []model.Report{}
	for rows.Next() {
		var data []byte
		var compressed int
		if err = rows.Scan(&data, &compressed); err != nil {
			return nil, err
		}
		if compressed == 1 {
			data, err = unpackBlock(data, 1)
			if err != nil {
				return nil, err
			}
		}
		var r model.Report
		if err = json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].SiteID < reports[j].SiteID })
	return reports, rows.Err()
}

func (s *Store) Query(ctx context.Context, host, site, kind string, since time.Time, limit int) ([]Record, error) {
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	return s.queryBlocks(ctx, host, site, kind, since, limit, false, false)
}

func (s *Store) Oldest(ctx context.Context, host, site, kind string, since time.Time) (*Record, error) {
	records, err := s.queryBlocks(ctx, host, site, kind, since, 1, true, true)
	if err != nil || len(records) == 0 {
		return nil, err
	}
	return &records[0], nil
}

func (s *Store) LatestEntities(ctx context.Context, host, kind string) ([]Record, error) {
	// Incidents have an explicit, small latest-entity index. Decode only blocks
	// containing those heads, regardless of an incident's age.
	if kind == "incident" {
		rows, err := s.DB.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,COALESCE(a.data,c.data),s.host_id,s.site_id,s.kind,h.record_id
 FROM incident_heads h JOIN streams s ON s.host_id=h.host_id AND s.kind='incident' JOIN blocks b ON b.stream_id=s.id AND b.first_id<=h.record_id AND b.last_id>=h.record_id LEFT JOIN active_payloads a ON a.block_id=b.id LEFT JOIN sealed_payloads c ON c.block_id=b.id WHERE h.host_id=? ORDER BY h.record_id DESC`, host)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Record{}
		for rows.Next() {
			var b block
			var id int64
			if err = rows.Scan(&b.id, &b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &b.data, &b.host, &b.site, &b.kind, &id); err != nil {
				return nil, err
			}
			samples, err := b.observations()
			if err != nil {
				return nil, err
			}
			for _, sample := range samples {
				if sample.id == id {
					out = append(out, Record{ID: id, HostID: host, SiteID: b.site, Kind: kind, ObservedAt: sample.at, Data: sample.data})
					break
				}
			}
			if len(out) >= 1000 {
				break
			}
		}
		return out, rows.Err()
	}
	records, err := s.queryBlocks(ctx, host, "", kind, time.Time{}, int(^uint(0)>>1), false, false)
	if err != nil {
		return nil, err
	}
	out := []Record{}
	seen := map[string]bool{}
	for _, r := range records {
		var value struct {
			ID json.RawMessage `json:"id"`
		}
		if err = json.Unmarshal(r.Data, &value); err != nil {
			return nil, err
		}
		key := string(value.ID)
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
		if len(out) >= 1000 {
			break
		}
	}
	return out, nil
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
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO dedup(host_id,kind,event_key,observed_at) VALUES(?,?,?,?)`, host, kind, key, at.UTC().Format(timestampLayout))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	id, err := insertObservation(ctx, tx, host, site, kind, at, data, 0)
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dedup SET record_id=? WHERE host_id=? AND kind=? AND event_key=?`, id, host, kind, key); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) State(ctx context.Context, host, site, kind string) (json.RawMessage, error) {
	var data []byte
	err := s.DB.QueryRowContext(ctx, `SELECT data FROM state WHERE host_id=? AND site_id=? AND kind=?`, host, site, kind).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw, err := unpackBlock(data, 1)
	return json.RawMessage(raw), err
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
	if err = putState(ctx, tx, host, site, stateKind, data); err != nil {
		return err
	}
	if string(eventData) != "null" {
		if _, err = insertObservation(ctx, tx, host, site, eventKind, at, eventData, 0); err != nil {
			return err
		}
	}
	return tx.Commit()
}
