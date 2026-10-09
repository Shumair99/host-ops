package config

import "fmt"

// History controls local SQLite storage; the optional VPS ring has its own fixed limits.
type History struct {
	RetentionDays int    `json:"retention_days,omitempty"`
	MetricsDays   int    `json:"metrics_days,omitempty"`
	MetricDetail  string `json:"metric_detail,omitempty"`
	EvidenceDays  int    `json:"evidence_days,omitempty"`
	MaxDBMB       int    `json:"max_db_mb,omitempty"`
}

func (h History) Defaults() History {
	if h.MetricDetail == "" {
		h.MetricDetail = "tiered"
	}
	if h.RetentionDays == 0 {
		h.RetentionDays = 7
	}
	if h.MetricsDays == 0 {
		h.MetricsDays = 30
		if h.MetricDetail == "full" {
			h.MetricsDays = 2
		}
	}
	if h.EvidenceDays == 0 {
		h.EvidenceDays = 365
	}
	if h.MaxDBMB == 0 {
		h.MaxDBMB = 256
	}
	return h
}

func (h History) Validate() error {
	if h.MetricDetail != "" && h.MetricDetail != "tiered" && h.MetricDetail != "full" {
		return fmt.Errorf("history.metric_detail must be tiered or full")
	}
	for _, days := range []int{h.RetentionDays, h.MetricsDays, h.EvidenceDays} {
		if days < 0 || days > 3650 {
			return fmt.Errorf("history retention must be 1..3650 days, or zero for defaults")
		}
	}
	if h.MaxDBMB != 0 && (h.MaxDBMB < 16 || h.MaxDBMB > 65536) {
		return fmt.Errorf("history.max_db_mb must be 16..65536, or zero for the 256 MiB default")
	}
	return nil
}
