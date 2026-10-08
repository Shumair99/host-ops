package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

func recordLabel(r storage.Record, sites []config.Site) string {
	var d map[string]any
	json.Unmarshal(r.Data, &d)
	if name, _ := d["name"].(string); name != "" {
		return clean(name)
	}
	for _, s := range sites {
		if s.ID == r.SiteID {
			if s.Proxy.Domain != "" {
				return clean(s.Proxy.Domain)
			}
			return clean(s.Name)
		}
	}
	if r.SiteID == "" {
		return "host"
	}
	return clean(r.SiteID)
}

func activitySummary(r storage.Record) string {
	var d map[string]any
	json.Unmarshal(r.Data, &d)
	str := func(k string) string { s, _ := d[k].(string); return clean(s) }
	before, after, kind := str("before"), str("after"), str("type")
	if r.Kind == "deployment" {
		return "Deployment: " + str("status") + "  revision: " + str("commit")
	}
	if r.Kind == "incident" {
		return "Incident: " + str("status") + " " + str("summary")
	}
	label := map[string]string{"health_changed": "Health", "runtime_changed": "Runtime", "image_changed": "Image", "container_recreated": "Started", "proxy_changed": "Proxy"}[kind]
	if r.Kind == "change" {
		label = "Changed: " + str("field")
	}
	if label != "" {
		if (before == "unknown" || before == "") && kind == "health_changed" {
			return "Health observed: " + after
		}
		return fmt.Sprintf("%s: %s → %s", label, before, after)
	}
	return recordSummary(r.Data)
}

// Event and change records can describe the same transition. Keep one visible
// activity per site, timestamp and field while retaining the underlying history.
func mergedActivity(records []storage.Record, limit int) []storage.Record {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].ObservedAt.Equal(records[j].ObservedAt) {
			return records[i].Kind == "event" && records[j].Kind != "event"
		}
		return records[i].ObservedAt.After(records[j].ObservedAt)
	})
	seen, out := map[string]bool{}, []storage.Record{}
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

func (s Source) activity(ctx context.Context, siteID string, limit int) ([]storage.Record, error) {
	records := []storage.Record{}
	for _, kind := range []string{"event", "change", "deployment", "incident"} {
		more, err := s.Store.Query(ctx, s.Config.Host.ID, siteID, kind, time.Now().Add(-24*time.Hour), limit)
		if err != nil {
			return nil, err
		}
		records = append(records, more...)
	}
	return mergedActivity(records, limit), nil
}

func timelineLabels(sites []config.Site) map[string]string {
	labels := map[string]string{}
	for _, s := range sites {
		labels[s.ID] = clean(s.Proxy.Domain)
		if strings.TrimSpace(labels[s.ID]) == "" {
			labels[s.ID] = clean(s.Name)
		}
	}
	return labels
}
