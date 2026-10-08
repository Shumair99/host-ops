package incidents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Incident struct {
	ID         string            `json:"id"`
	HostID     string            `json:"host_id"`
	SiteIDs    []string          `json:"site_ids"`
	Source     string            `json:"source"`
	Status     string            `json:"status"`
	StartedAt  time.Time         `json:"started_at"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
	Dependency string            `json:"dependency"`
	Symptoms   []model.Check     `json:"symptoms"`
	Diagnoses  []model.Diagnosis `json:"diagnoses"`
}

type tracking struct {
	Failures     int       `json:"failures"`
	Recoveries   int       `json:"recoveries"`
	FirstFailure time.Time `json:"first_failure"`
	Active       *Incident `json:"active,omitempty"`
}

func Observe(ctx context.Context, store *storage.Store, r model.Report, source string, failureThreshold, recoveryThreshold int) (*Incident, error) {
	if failureThreshold < 1 {
		failureThreshold = 3
	}
	if recoveryThreshold < 1 {
		recoveryThreshold = 2
	}
	var state tracking
	data, err := store.State(ctx, r.HostID, r.SiteID, "incident:"+source)
	if err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
	}
	var transition *Incident
	if r.Overall == "critical" {
		state.Recoveries = 0
		if state.Failures == 0 {
			state.FirstFailure = r.ObservedAt
		}
		state.Failures++
		if state.Active == nil && state.Failures >= failureThreshold {
			bytes := make([]byte, 8)
			if _, err = rand.Read(bytes); err != nil {
				return nil, err
			}
			incident := Incident{ID: "INC-" + hex.EncodeToString(bytes), HostID: r.HostID, SiteIDs: []string{r.SiteID}, Source: source, Status: "open", StartedAt: state.FirstFailure, Dependency: "site", Diagnoses: r.Diagnoses, Symptoms: []model.Check{}}
			for _, c := range r.Checks {
				if c.State == model.Fail {
					incident.Symptoms = append(incident.Symptoms, c)
				}
			}
			state.Active = &incident
			transition = &incident
		}
	} else if r.Overall == "healthy" || r.Overall == "warning" {
		state.Failures = 0
		state.Recoveries++
		if state.Active != nil && state.Recoveries >= recoveryThreshold {
			resolved := *state.Active
			at := r.ObservedAt
			resolved.Status = "resolved"
			resolved.ResolvedAt = &at
			state.Active = nil
			transition = &resolved
		}
	} else {
		// Unknown observations neither open nor close an incident and break consecutive streaks.
		state.Failures = 0
		state.Recoveries = 0
	}
	return transition, store.Transition(ctx, r.HostID, r.SiteID, "incident:"+source, state, "incident", r.ObservedAt, transition)
}
