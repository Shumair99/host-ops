package watcher

import (
	"context"
	"time"

	"github.com/Shumair99/host-ops/internal/alerts"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/health"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func Cycle(ctx context.Context, c config.Config, store *storage.Store, failures, recoveries int, webhook string) ([]model.Report, error) {
	if err := store.Prune(ctx, c.History); err != nil {
		return nil, err
	}
	reports := []model.Report{}
	for _, site := range c.Sites {
		r := health.External(ctx, site)
		r.Diagnoses = diagnostics.Diagnose(r)
		reports = append(reports, r)
		if _, err := store.Add(ctx, r.HostID, r.SiteID, "external_health", r.ObservedAt, r); err != nil {
			return nil, err
		}
	}
	for _, r := range incidents.GroupReports(c, reports, "watcher") {
		incident, err := incidents.Observe(ctx, store, r, "watcher", failures, recoveries)
		if err != nil {
			return nil, err
		}
		if incident != nil && webhook != "" {
			delivery := "delivered"
			issue := ""
			if err := alerts.Notify(ctx, webhook, *incident); err != nil {
				delivery = "failed"
				issue = err.Error()
			}
			if _, err = store.Add(ctx, r.HostID, r.SiteID, "alert", time.Now().UTC(), map[string]string{"host_id": r.HostID, "site_id": r.SiteID, "incident_id": incident.ID, "status": incident.Status, "delivery": delivery, "issue": issue}); err != nil {
				return nil, err
			}
		}
	}
	return reports, store.Prune(ctx, c.History)
}
