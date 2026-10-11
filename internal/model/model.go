package model

import "time"

type State string

const (
	Pass    State = "pass"
	Warn    State = "warn"
	Fail    State = "fail"
	Unknown State = "unknown"
	Skip    State = "skip"
)

type Check struct {
	Code       string    `json:"code,omitempty"`
	Details    []string  `json:"details,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitzero"`
	Dependency string    `json:"dependency,omitempty"`
	HostID     string    `json:"host_id"`
	SiteID     string    `json:"site_id"`
	Layer      string    `json:"layer"`
	State      State     `json:"state"`
	Summary    string    `json:"summary"`
	LatencyMS  float64   `json:"latency_ms,omitempty"`
	StatusCode int       `json:"status_code,omitempty"`
}

type Runtime struct {
	HostID          string            `json:"host_id"`
	SiteID          string            `json:"site_id"`
	Type            string            `json:"type"`
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	Health          string            `json:"health"`
	Image           string            `json:"image,omitempty"`
	ImageID         string            `json:"image_id,omitempty"`
	Commit          string            `json:"commit,omitempty"`
	RestartPolicy   string            `json:"restart_policy,omitempty"`
	RestartCount    int               `json:"restart_count"`
	ExitCode        int               `json:"exit_code"`
	StartedAt       string            `json:"started_at,omitempty"`
	IPs             map[string]string `json:"ips,omitempty"`
	Ports           map[string][]Port `json:"ports,omitempty"`
	User            string            `json:"user,omitempty"`
	PostureObserved bool              `json:"posture_observed,omitempty"`
	Processes       ProcessSnapshot   `json:"processes,omitempty"`
	Privileged      bool              `json:"privileged"`
	ReadOnly        bool              `json:"read_only"`
	MemoryLimit     int64             `json:"memory_limit"`
	Mounts          []Mount           `json:"mounts,omitempty"`
}

// Only process identities and recognized roles are retained. Arguments and
// environment values never leave the collector.
type ProcessSnapshot struct {
	Observed bool              `json:"observed"`
	Complete bool              `json:"complete"`
	Items    []ProcessIdentity `json:"items,omitempty"`
	Issue    string            `json:"issue,omitempty"`
}
type ProcessIdentity struct {
	PID       int    `json:"pid"`
	ParentPID int    `json:"parent_pid"`
	UID       int    `json:"uid"`
	Name      string `json:"name"`
	Role      string `json:"role,omitempty"`
}

type Port struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type Mount struct {
	Type        string `json:"Type"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type Diagnosis struct {
	HostID        string   `json:"host_id"`
	SiteID        string   `json:"site_id"`
	Finding       string   `json:"finding"`
	Evidence      []string `json:"evidence"`
	Confidence    string   `json:"confidence"`
	Layer         string   `json:"layer"`
	Investigation string   `json:"investigation"`
}

type Report struct {
	HostID          string      `json:"host_id"`
	SiteID          string      `json:"site_id"`
	Name            string      `json:"name"`
	ObservedAt      time.Time   `json:"observed_at"`
	Vantage         string      `json:"vantage"`
	Overall         string      `json:"overall"`
	Runtime         Runtime     `json:"runtime"`
	Checks          []Check     `json:"checks"`
	Diagnoses       []Diagnosis `json:"diagnoses"`
	AffectedSiteIDs []string    `json:"affected_site_ids,omitempty"`
	Dependency      string      `json:"dependency,omitempty"`
}

func Overall(checks []Check) string {
	unknown, warning, passed := false, false, false
	for _, c := range checks {
		switch c.State {
		case Fail:
			return "critical"
		case Warn:
			warning = true
		case Unknown:
			unknown = true
		case Pass:
			passed = true
		}
	}
	if unknown {
		return "unknown"
	}
	if warning {
		return "warning"
	}
	if passed {
		return "healthy"
	}
	return "unknown"
}
