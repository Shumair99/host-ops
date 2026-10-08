package model

import "time"

const DefaultMaxAge = 3 * time.Minute

func Fresh(at, now time.Time, age time.Duration) bool {
	if age <= 0 {
		age = DefaultMaxAge
	}
	return !at.IsZero() && !at.After(now.Add(time.Minute)) && now.Sub(at) <= age
}

// Current preserves the original timestamp and never turns expired evidence into health.
func Current(r Report, now time.Time, age time.Duration) Report {
	if Fresh(r.ObservedAt, now, age) {
		return r
	}
	r.Checks = append([]Check(nil), r.Checks...)
	for i := range r.Checks {
		if r.Checks[i].State != Skip {
			r.Checks[i].State = Unknown
			r.Checks[i].Summary = "Stale observation: " + r.Checks[i].Summary
		}
	}
	r.Checks = append(r.Checks, Check{HostID: r.HostID, SiteID: r.SiteID, Layer: "collector", State: Unknown, Summary: "Fresh observations unavailable"})
	r.Overall = "unknown"
	r.Diagnoses = nil
	return r
}
