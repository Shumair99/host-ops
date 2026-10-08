// Package evidence validates operator/pipeline records received through the CLI.
package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/deployments"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/incidents"
	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/recovery"
	"github.com/Shumair99/host-ops/internal/storage"
	"io"
	"strings"
	"time"
)

func Decode(reader io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(reader, 256*1024+1))
	if err != nil {
		return fmt.Errorf("evidence input unavailable")
	}
	if len(data) > 256*1024 {
		return fmt.Errorf("evidence exceeds 256 KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid evidence JSON: %w", err)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("one JSON value required")
	}
	return nil
}

func Record(ctx context.Context, c config.Config, store *storage.Store, kind string, input io.Reader) (int, error) {
	now := time.Now().UTC()
	var value any
	var site, id, status string
	switch kind {
	case "deployment":
		var d deployments.Deployment
		if err := Decode(input, &d); err != nil {
			return 0, err
		}
		if d.HealthResult == "" {
			d.HealthResult = "unknown"
		}
		if err := d.Validate(); err != nil {
			return 0, err
		}
		d.HostID = c.Host.ID
		d.Origin = "deployment_notification"
		d.ObservedAt = now
		value = d
		site, id, status = d.SiteID, d.ID, d.Status
	case "backup":
		var b recovery.Backup
		if err := Decode(input, &b); err != nil {
			return 0, err
		}
		if err := b.Validate(now); err != nil {
			return 0, err
		}
		b.HostID = c.Host.ID
		b.Origin = "operator_report"
		b.ObservedAt = now
		value = b
		site, id, status = b.SiteID, b.ID, b.Status
	case "restore":
		var r recovery.Restore
		if err := Decode(input, &r); err != nil {
			return 0, err
		}
		if err := r.Validate(now); err != nil {
			return 0, err
		}
		r.HostID = c.Host.ID
		r.Origin = "operator_report"
		r.ObservedAt = now
		r.MeasuredRTOseconds = r.CompletedAt.Sub(r.StartedAt).Seconds()
		value = r
		site, id, status = r.SiteID, r.ID, r.Status
	case "observations":
		var reports []model.Report
		if err := Decode(input, &reports); err != nil {
			return 0, err
		}
		return Observe(ctx, c, store, reports, now)
	default:
		return 0, fmt.Errorf("record kind must be deployment, backup, restore or observations")
	}
	if site == "" {
		return 0, fmt.Errorf("site_id required")
	}
	if _, err := c.Select(site); err != nil {
		return 0, err
	}
	created, err := store.AddOnce(ctx, c.Host.ID, site, kind, site+":"+id+":"+status, now, value)
	if created {
		return 1, err
	}
	return 0, err
}

func Observe(ctx context.Context, c config.Config, store *storage.Store, reports []model.Report, now time.Time) (int, error) {
	if len(reports) == 0 || len(reports) > len(c.Sites) {
		return 0, fmt.Errorf("invalid observation batch")
	}
	unlock := store.LockState(c.Host.ID + ":observation-batch")
	defer unlock()
	seen := map[string]bool{}
	for i := range reports {
		r := &reports[i]
		sites, err := c.Select(r.SiteID)
		if r.SiteID == "" || err != nil || seen[r.SiteID] || r.ObservedAt.After(now.Add(time.Minute)) || r.ObservedAt.Before(now.Add(-15*time.Minute)) {
			return 0, fmt.Errorf("unknown, duplicate or stale site observation")
		}
		seen[r.SiteID] = true
		r.HostID = c.Host.ID
		r.Name = sites[0].Name
		r.Vantage = "external watcher report"
		r.Runtime = model.Runtime{HostID: r.HostID, SiteID: r.SiteID, Type: "external", Status: "not_applicable"}
		r.Dependency = ""
		r.AffectedSiteIDs = nil
		layers := map[string]bool{}
		for j := range r.Checks {
			check := &r.Checks[j]
			if check.Layer != "dns" && check.Layer != "tcp" && check.Layer != "tls" && check.Layer != "public_http" {
				return 0, fmt.Errorf("invalid external layer")
			}
			if layers[check.Layer] {
				return 0, fmt.Errorf("duplicate external layer")
			}
			layers[check.Layer] = true
			if check.State != model.Pass && check.State != model.Fail && check.State != model.Warn && check.State != model.Unknown && check.State != model.Skip {
				return 0, fmt.Errorf("invalid external check state")
			}
			if check.State == model.Skip && (check.Layer != "tls" || strings.HasPrefix(sites[0].Health.PublicURL, "https:")) {
				return 0, fmt.Errorf("check cannot be skipped")
			}
			if check.Layer == "public_http" && check.State == model.Pass && check.StatusCode != sites[0].Health.ExpectedStatus {
				return 0, fmt.Errorf("HTTP pass must match expected status")
			}
			if len(check.Summary) > 512 {
				return 0, fmt.Errorf("summary exceeds 512 characters")
			}
			check.Summary = logs.Redact(check.Summary)
			check.HostID = r.HostID
			check.SiteID = r.SiteID
		}
		if len(layers) != 4 {
			return 0, fmt.Errorf("DNS, TCP, TLS and public HTTP checks required")
		}
		r.Overall = model.Overall(r.Checks)
		r.Diagnoses = diagnostics.Diagnose(*r)
	}
	accepted := []model.Report{}
	for _, r := range reports {
		created, err := store.AddOnce(ctx, r.HostID, r.SiteID, "external_health", r.SiteID+":"+r.ObservedAt.UTC().Format(time.RFC3339Nano), r.ObservedAt, r)
		if err != nil {
			return len(accepted), err
		}
		if created {
			accepted = append(accepted, r)
		}
	}
	for _, r := range incidents.GroupReports(c, accepted, "watcher") {
		if _, err := incidents.Observe(ctx, store, r, "watcher", 3, 2); err != nil {
			return len(accepted), err
		}
	}
	return len(accepted), nil
}
