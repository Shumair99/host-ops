package deployments

import (
	"fmt"
	"math"
	"regexp"
	"time"
)

type Deployment struct {
	HostID          string    `json:"host_id"`
	SiteID          string    `json:"site_id"`
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	Commit          string    `json:"commit"`
	Image           string    `json:"image,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	DurationSeconds float64   `json:"duration_seconds,omitempty"`
	HealthResult    string    `json:"health_result"`
	FailureReason   string    `json:"failure_reason,omitempty"`
	Origin          string    `json:"origin"`
	ObservedAt      time.Time `json:"observed_at"`
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
var sha = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)
var image = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./:@-]{0,255}$`)
var branch = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_./-]{0,127}$`)

func (d Deployment) Validate() error {
	if !safeID.MatchString(d.ID) || !safeID.MatchString(d.SiteID) || !sha.MatchString(d.Commit) {
		return fmt.Errorf("valid deployment id, site_id and commit are required")
	}
	if d.Status != "started" && d.Status != "completed" && d.Status != "failed" {
		return fmt.Errorf("status must be started, completed or failed")
	}
	if d.Image != "" && !image.MatchString(d.Image) {
		return fmt.Errorf("invalid image reference")
	}
	if d.Branch != "" && !branch.MatchString(d.Branch) {
		return fmt.Errorf("invalid branch")
	}
	if math.IsNaN(d.DurationSeconds) || d.DurationSeconds < 0 || d.DurationSeconds > 86400 {
		return fmt.Errorf("duration must be 0..86400 seconds")
	}
	if d.HealthResult != "unknown" && d.HealthResult != "healthy" && d.HealthResult != "unhealthy" {
		return fmt.Errorf("health_result must be unknown, healthy or unhealthy")
	}
	if d.FailureReason != "" && d.FailureReason != "build_failed" && d.FailureReason != "health_failed" && d.FailureReason != "script_failed" && d.FailureReason != "unknown" {
		return fmt.Errorf("failure_reason must be a supported category; do not send raw logs")
	}
	return nil
}
