package watcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	reports := []model.Report{}
	for _, site := range c.Sites {
		r := health.External(ctx, site)
		r.Diagnoses = diagnostics.Diagnose(r)
		reports = append(reports, r)
		if _, err := store.Add(ctx, r.HostID, r.SiteID, "external_health", r.ObservedAt, r); err != nil {
			return nil, err
		}
		incident, err := incidents.Observe(ctx, store, r, "watcher", failures, recoveries)
		if err != nil {
			return nil, err
		}
		if incident != nil && webhook != "" {
			delivery := "delivered"
			if alerts.Notify(ctx, webhook, *incident) != nil {
				delivery = "failed"
			}
			if _, err = store.Add(ctx, r.HostID, r.SiteID, "alert", time.Now().UTC(), map[string]string{"host_id": r.HostID, "site_id": r.SiteID, "incident_id": incident.ID, "delivery": delivery}); err != nil {
				return nil, err
			}
		}
	}
	return reports, nil
}

func Publish(ctx context.Context, endpoint, token string, reports []model.Report) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid agent URL")
	}
	if u.Scheme == "http" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		return fmt.Errorf("HTTP agent URL must use a loopback tunnel")
	}
	data, err := json.Marshal(reports)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/api/observations", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("agent unreachable; local watcher history retained")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		return fmt.Errorf("agent rejected observations")
	}
	return nil
}
