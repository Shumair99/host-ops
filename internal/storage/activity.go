package storage

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

func (s *Store) Activity(ctx context.Context, host, site string, since time.Time, limit int) ([]Record, error) {
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	records := []Record{}
	for _, kind := range []string{"event", "change", "deployment", "incident"} {
		rows, err := s.Query(ctx, host, site, kind, since, limit)
		if err != nil {
			return nil, err
		}
		records = append(records, rows...)
	}
	return MergeActivity(records, limit), nil
}

func MergeActivity(records []Record, limit int) []Record {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].ObservedAt.Equal(records[j].ObservedAt) {
			return records[i].Kind == "event" && records[j].Kind != "event"
		}
		return records[i].ObservedAt.After(records[j].ObservedAt)
	})
	seen, out := map[string]bool{}, []Record{}
	for _, r := range records {
		var d map[string]any
		json.Unmarshal(r.Data, &d)
		field, _ := d["field"].(string)
		kind, _ := d["type"].(string)
		if r.Kind == "event" {
			field = map[string]string{"health_changed": "health", "runtime_changed": "runtime.status", "image_changed": "runtime.image_id", "container_recreated": "runtime.started_at", "proxy_changed": "proxy_route"}[kind]
		}
		if field == "" {
			field = r.Kind + ":" + kind
		}
		key := r.SiteID + "/" + r.ObservedAt.Format(time.RFC3339Nano) + "/" + field
		if !seen[key] {
			out = append(out, r)
			seen[key] = true
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}
