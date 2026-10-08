package storage

import (
	"context"
	"database/sql"
	"time"
)

// The database lease prevents two dashboard processes sharing this history
// from launching the same expensive inspection. A crashed owner expires.
func (s *Store) AcquireLease(ctx context.Context, host, kind, owner string, now time.Time, ttl time.Duration) (bool, error) {
	var result string
	err := s.DB.QueryRowContext(ctx, `INSERT INTO leases(host_id,kind,owner,expires_at) VALUES(?,?,?,?) ON CONFLICT(host_id,kind) DO UPDATE SET owner=excluded.owner,expires_at=excluded.expires_at WHERE leases.expires_at<=? RETURNING owner`, host, kind, owner, now.Add(ttl).UnixNano(), now.UnixNano()).Scan(&result)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
func (s *Store) ReleaseLease(ctx context.Context, host, kind, owner string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM leases WHERE host_id=? AND kind=? AND owner=?", host, kind, owner)
	return err
}
