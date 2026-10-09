package metrics

import "time"

// Statistic describes measured values only. Count can be smaller than the
// window's Samples when a metric was unavailable; absence is never a zero.
type Statistic struct {
	Count   int       `json:"count"`
	Mean    float64   `json:"mean"`
	Min     float64   `json:"min"`
	Max     float64   `json:"max"`
	First   float64   `json:"first"`
	Last    float64   `json:"last"`
	FirstAt time.Time `json:"first_at"`
	LastAt  time.Time `json:"last_at"`
}

// HistoryPoint is an individual reading (ResolutionSeconds=0) or a labelled
// summary. ObservedSeconds lists seconds with actual observations, not seconds
// assumed covered between readings. It survives merging and collector gaps.
type HistoryPoint struct {
	HostID            string               `json:"host_id"`
	SiteID            string               `json:"site_id"`
	Start             time.Time            `json:"start"`
	End               time.Time            `json:"end"`
	ResolutionSeconds int                  `json:"resolution_seconds"`
	Samples           int                  `json:"samples"`
	FirstAt           time.Time            `json:"first_at"`
	LastAt            time.Time            `json:"last_at"`
	ObservedSeconds   []int                `json:"observed_seconds"`
	Values            map[string]Statistic `json:"values"`
	Unavailable       map[string]int       `json:"unavailable,omitempty"`
}
