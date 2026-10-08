package tui

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s Source) activity(ctx context.Context, siteID string, limit int) ([]storage.Record, error) {
	return s.Store.Activity(ctx, s.Config.Host.ID, siteID, time.Now().Add(-24*time.Hour), limit)
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
