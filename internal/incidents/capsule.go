package incidents

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/Shumair99/host-ops/internal/logs"
	"github.com/Shumair99/host-ops/internal/storage"
)

func List(ctx context.Context, store *storage.Store, host, site string) ([]storage.Record, error) {
	records, err := store.LatestEntities(ctx, host, "incident")
	if err != nil {
		return nil, err
	}
	out := []storage.Record{}
	for _, r := range records {
		var i Incident
		if err = json.Unmarshal(r.Data, &i); err != nil {
			return nil, err
		}
		if site == "" {
			out = append(out, r)
			continue
		}
		for _, id := range i.SiteIDs {
			if id == site {
				out = append(out, r)
				break
			}
		}
	}
	return out, nil
}

type Capsule struct {
	HostID      string           `json:"host_id"`
	GeneratedAt time.Time        `json:"generated_at"`
	Origin      string           `json:"origin"`
	Incident    Incident         `json:"incident"`
	Timeline    []storage.Record `json:"timeline"`
	Notes       []string         `json:"notes"`
}

var address = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
var privatePath = regexp.MustCompile(`(?:[A-Za-z]:[\\/][^\s",]+|/(?:home|srv|var|opt|root|etc|mnt|tmp)/[^\s",]+)`)

func sanitize(value any) any {
	switch v := value.(type) {
	case string:
		return privatePath.ReplaceAllString(address.ReplaceAllString(logs.Redact(v), "[address]"), "[path]")
	case []any:
		for i := range v {
			v[i] = sanitize(v[i])
		}
		return v
	case map[string]any:
		for k, item := range v {
			v[k] = sanitize(item)
		}
		return v
	default:
		return value
	}
}

func Export(ctx context.Context, store *storage.Store, host, id string) (Capsule, error) {
	records, err := List(ctx, store, host, "")
	if err != nil {
		return Capsule{}, err
	}
	var incident Incident
	found := false
	for _, record := range records {
		var i Incident
		if json.Unmarshal(record.Data, &i) == nil && i.ID == id {
			incident = i
			found = true
			break
		}
	}
	if !found {
		return Capsule{}, fmt.Errorf("incident not found")
	}
	until := time.Now().UTC()
	if incident.ResolvedAt != nil {
		until = incident.ResolvedAt.Add(5 * time.Minute)
	}
	c := Capsule{HostID: host, GeneratedAt: time.Now().UTC(), Origin: "hostops_recorded_evidence", Incident: incident, Timeline: []storage.Record{}, Notes: []string{"Contains observed incident symptoms and selected events, changes and deployment notifications; no raw logs, environment values or runtime/configuration dumps.", "Credential patterns, IPv4 addresses and common private paths are redacted. Site IDs, host IDs and public names remain; review before sharing.", "Temporal correlation is evidence of association, not proof of causation. Up to 100 recent records of each timeline kind are included."}}
	allowed := map[string]bool{}
	for _, site := range incident.SiteIDs {
		allowed[site] = true
	}
	for _, kind := range []string{"event", "change", "deployment"} {
		items, err := store.Query(ctx, host, "", kind, incident.StartedAt.Add(-5*time.Minute), 100)
		if err != nil {
			return Capsule{}, err
		}
		for _, item := range items {
			if !item.ObservedAt.After(until) && (allowed[item.SiteID] || item.SiteID == "") {
				c.Timeline = append(c.Timeline, item)
			}
		}
	}
	sort.Slice(c.Timeline, func(i, j int) bool { return c.Timeline[i].ObservedAt.Before(c.Timeline[j].ObservedAt) })
	data, err := json.Marshal(c)
	if err != nil {
		return Capsule{}, err
	}
	var value any
	if err = json.Unmarshal(data, &value); err != nil {
		return Capsule{}, err
	}
	data, err = json.Marshal(sanitize(value))
	if err != nil {
		return Capsule{}, err
	}
	err = json.Unmarshal(data, &c)
	return c, err
}
