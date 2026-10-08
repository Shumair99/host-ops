// Package discovery combines host, runtime and routing observations into one inventory.
package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const ProtocolVersion = 1

type Source struct {
	ProxyID    string    `json:"proxy_id,omitempty"`
	ID         string    `json:"id"`
	ParentID   string    `json:"parent_id,omitempty"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"` // ready, absent, unavailable
	Mode       string    `json:"mode"`  // active or configured
	ObservedAt time.Time `json:"observed_at"`
	Issue      string    `json:"issue,omitempty"`
}

type Network struct {
	IP      string   `json:"ip"`
	Aliases []string `json:"aliases,omitempty"`
}

type Mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type Workload struct {
	ID            string              `json:"id"`
	InstanceID    string              `json:"instance_id,omitempty"`
	SourceID      string              `json:"source_id"`
	Type          string              `json:"type"`
	Name          string              `json:"name"`
	Project       string              `json:"project,omitempty"`
	Service       string              `json:"service,omitempty"`
	Status        string              `json:"status"`
	Health        string              `json:"health,omitempty"`
	Image         string              `json:"image,omitempty"`
	Revision      string              `json:"revision,omitempty"`
	RestartPolicy string              `json:"restart_policy,omitempty"`
	Networks      map[string]Network  `json:"networks,omitempty"`
	Ports         map[string][]string `json:"ports,omitempty"`
	Mounts        []Mount             `json:"mounts,omitempty"`
	PID           int                 `json:"pid,omitempty"`
	ProxyKind     string              `json:"proxy_kind,omitempty"`
}

type Listener struct {
	Address string `json:"address"`
	PID     int    `json:"pid,omitempty"`
	Unit    string `json:"unit,omitempty"`
}

type Route struct {
	ID         string   `json:"id"`
	SourceID   string   `json:"source_id"`
	ProxyID    string   `json:"proxy_id,omitempty"`
	Hosts      []string `json:"hosts"`
	Paths      []string `json:"paths,omitempty"`
	Kind       string   `json:"kind"`
	Scheme     string   `json:"scheme"`
	Port       string   `json:"port,omitempty"`
	Upstreams  []string `json:"upstreams,omitempty"`
	Root       string   `json:"root,omitempty"`
	RedirectTo string   `json:"redirect_to,omitempty"`
	Networks   []string `json:"networks,omitempty"`
	Note       string   `json:"note,omitempty"`
}

type Snapshot struct {
	HostID       string     `json:"host_id"`
	ObservedAt   time.Time  `json:"observed_at"`
	OS           string     `json:"os"`
	Architecture string     `json:"architecture"`
	Sources      []Source   `json:"sources"`
	Workloads    []Workload `json:"workloads"`
	Listeners    []Listener `json:"listeners,omitempty"`
	Routes       []Route    `json:"routes"`
}

type Site struct {
	ID           string    `json:"id"`
	Domain       string    `json:"domain"`
	Routes       []Route   `json:"routes"`
	WorkloadIDs  []string  `json:"workload_ids"`
	State        string    `json:"state"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	MissingSince time.Time `json:"missing_since,omitempty"`
	MissingScans int       `json:"missing_scans,omitempty"`
}

type Registry struct {
	Snapshot Snapshot `json:"snapshot"`
	Sites    []Site   `json:"sites"`
	Patterns []Route  `json:"patterns"`
}

func StableID(kind, key string) string {
	sum := sha256.Sum256([]byte(key))
	return kind + "-" + hex.EncodeToString(sum[:12])
}
