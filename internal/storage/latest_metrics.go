package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// LatestMetrics reads one bounded block per property stream. A busy stream's
// history must not exhaust a global row budget and hide another property's
// latest reading. This does not walk or modify historical blocks.
func (s *Store) LatestMetrics(ctx context.Context, host string) ([]Record, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT b.id,b.stream_id,b.first_id,b.last_id,b.first_at,b.last_at,b.samples,b.encoding,COALESCE(a.data,c.data),s.host_id,s.site_id,s.kind,s.latest_id
 FROM streams s JOIN blocks b ON b.stream_id=s.id AND b.first_id<=s.latest_id AND b.last_id>=s.latest_id LEFT JOIN active_payloads a ON a.block_id=b.id LEFT JOIN sealed_payloads c ON c.block_id=b.id
 WHERE s.host_id=? AND s.kind='metrics' ORDER BY s.latest_id DESC LIMIT 4096`, host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var b block
		var id int64
		if err := rows.Scan(&b.id, &b.streamID, &b.firstID, &b.lastID, &b.firstAt, &b.lastAt, &b.count, &b.encoding, &b.data, &b.host, &b.site, &b.kind, &id); err != nil {
			return nil, err
		}
		samples, err := b.observations()
		if err != nil {
			return nil, fmt.Errorf("read latest metric block: %w", err)
		}
		for _, sample := range samples {
			if sample.id == id {
				out = append(out, Record{ID: id, HostID: b.host, SiteID: b.site, Kind: b.kind, ObservedAt: sample.at, Data: sample.data})
				break
			}
		}
	}
	return out, rows.Err()
}
