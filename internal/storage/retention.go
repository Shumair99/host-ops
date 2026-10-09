package storage

import (
	"context"
	"encoding/json"
	"regexp"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

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
