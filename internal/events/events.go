package events

import (
	"context"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Event struct {
	HostID     string    `json:"host_id"`
	SiteID     string    `json:"site_id"`
	SiteName   string    `json:"name,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Type       string    `json:"type"`
	Summary    string    `json:"summary"`
	Before     string    `json:"before,omitempty"`
	After      string    `json:"after,omitempty"`
}

func Between(before, after model.Report) []Event {
	out := []Event{}
	add := func(kind, summary, old, new string) {
		if old != new {
			out = append(out, Event{HostID: after.HostID, SiteID: after.SiteID, SiteName: after.Name, ObservedAt: after.ObservedAt, Type: kind, Summary: summary, Before: old, After: new})
		}
	}
	add("health_changed", "Site health changed", before.Overall, after.Overall)
	add("image_changed", "Container image changed", before.Runtime.ImageID, after.Runtime.ImageID)
	add("container_recreated", "Container start time changed", before.Runtime.StartedAt, after.Runtime.StartedAt)
	add("runtime_changed", "Runtime state changed", before.Runtime.Status, after.Runtime.Status)
	oldRoutes, newRoutes := "", ""
	for _, c := range before.Checks {
		if c.Layer == "proxy_route" {
			oldRoutes = c.Summary
		}
	}
	for _, c := range after.Checks {
		if c.Layer == "proxy_route" {
			newRoutes = c.Summary
		}
	}
	add("proxy_changed", "Loaded proxy observation changed", oldRoutes, newRoutes)
	return out
}

func Record(ctx context.Context, store *storage.Store, before, after []model.Report) error {
	previous := map[string]model.Report{}
	for _, r := range before {
		previous[r.SiteID] = r
	}
	for _, r := range after {
		old, ok := previous[r.SiteID]
		if !ok {
			continue
		}
		for _, ev := range Between(old, r) {
			if _, err := store.Add(ctx, ev.HostID, ev.SiteID, "event", ev.ObservedAt, ev); err != nil {
				return err
			}
		}
	}
	return nil
}
