package changes

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Snapshot struct {
	SiteName   string            `json:"name,omitempty"`
	HostID     string            `json:"host_id"`
	SiteID     string            `json:"site_id"`
	ObservedAt time.Time         `json:"observed_at"`
	Values     map[string]string `json:"values"`
}
type Change struct {
	SiteName   string    `json:"name,omitempty"`
	HostID     string    `json:"host_id"`
	SiteID     string    `json:"site_id"`
	ObservedAt time.Time `json:"observed_at"`
	Field      string    `json:"field"`
	Before     string    `json:"before"`
	After      string    `json:"after"`
	Type       string    `json:"type"`
}

func From(r model.Report) Snapshot {
	values := map[string]string{"health": r.Overall}
	if r.Runtime.Status != "" {
		values["runtime.status"] = r.Runtime.Status
		values["runtime.image"] = r.Runtime.Image
		values["runtime.image_id"] = r.Runtime.ImageID
		values["source.commit"] = r.Runtime.Commit
		values["runtime.started_at"] = r.Runtime.StartedAt
		values["runtime.restart_policy"] = r.Runtime.RestartPolicy
		values["runtime.restart_count"] = strconv.Itoa(r.Runtime.RestartCount)
		ports, _ := json.Marshal(r.Runtime.Ports)
		values["runtime.ports"] = string(ports)
	}
	for _, check := range r.Checks {
		if (check.Layer == "proxy_route" || check.Layer == "dns") && check.State != model.Unknown {
			values[check.Layer] = check.Summary
		}
	}
	return Snapshot{HostID: r.HostID, SiteID: r.SiteID, SiteName: r.Name, ObservedAt: r.ObservedAt, Values: values}
}

func Between(before, after Snapshot) []Change {
	keys := map[string]bool{}
	for k := range before.Values {
		keys[k] = true
	}
	for k := range after.Values {
		keys[k] = true
	}
	sorted := []string{}
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	changes := []Change{}
	for _, key := range sorted {
		old, oldOK := before.Values[key]
		new, newOK := after.Values[key]
		if old != new || oldOK != newOK {
			kind := "observed_change"
			if !oldOK || !newOK {
				kind = "observation_availability"
			}
			changes = append(changes, Change{HostID: after.HostID, SiteID: after.SiteID, SiteName: after.SiteName, ObservedAt: after.ObservedAt, Field: key, Before: old, After: new, Type: kind})
		}
	}
	return changes
}

func Record(ctx context.Context, store *storage.Store, reports []model.Report) error {
	for _, r := range reports {
		current := From(r)
		previous, err := store.Query(ctx, r.HostID, r.SiteID, "snapshot", time.Time{}, 1)
		if err != nil {
			return err
		}
		if len(previous) > 0 {
			var old Snapshot
			if err = json.Unmarshal(previous[0].Data, &old); err != nil {
				return err
			}
			for _, change := range Between(old, current) {
				if _, err = store.Add(ctx, r.HostID, r.SiteID, "change", r.ObservedAt, change); err != nil {
					return err
				}
			}
		}
		if _, err = store.Add(ctx, r.HostID, r.SiteID, "snapshot", r.ObservedAt, current); err != nil {
			return err
		}
	}
	return nil
}
