// Package vpsagent implements the small, Linux-only collector installed on a VPS.
// Its wire protocol is carried through the existing SSH connection; it opens no
// TCP listener and accepts only bounded host-metric history requests.
package vpsagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

const (
	ProtocolVersion = 1
	HistoryWindow   = 30 * time.Minute
	HistoryEvery    = 15 * time.Second
	SampleEvery     = time.Second
	MaxHistory      = 128
	MaxFrameBytes   = 256 * 1024
	MaxHistoryBytes = 256 * 1024
)

var hostIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type Request struct {
	Version int    `json:"version"`
	HostID  string `json:"host_id"`
}

type Frame struct {
	Version int              `json:"version"`
	HostID  string           `json:"host_id"`
	Kind    string           `json:"kind"`
	Sample  *metrics.Sample  `json:"sample,omitempty"`
	Samples []metrics.Sample `json:"samples,omitempty"`
}

type historyFile struct {
	Version int              `json:"version"`
	HostID  string           `json:"host_id"`
	Samples []metrics.Sample `json:"samples"`
}

var allowedValues = map[string]struct{}{
	"cpu_percent": {}, "memory_total_bytes": {}, "memory_used_bytes": {},
	"memory_percent": {}, "swap_used_bytes": {}, "uptime_seconds": {},
	"load_1": {}, "load_5": {}, "load_15": {}, "disk_total_bytes": {},
	"disk_used_bytes": {}, "disk_free_bytes": {}, "disk_percent": {},
	"disk_reserved_bytes": {},
}

var allowedUnavailable = map[string]struct{}{
	"cpu": {}, "memory": {}, "uptime": {}, "load": {}, "disk": {},
}

func ValidateRequest(r Request, hostID string) error {
	if r.Version != ProtocolVersion || !hostIDPattern.MatchString(hostID) || r.HostID != hostID {
		return fmt.Errorf("collector request rejected")
	}
	return nil
}

// ValidateSample treats all VPS-provided values as untrusted input. It rejects
// unexpected fields and non-finite or implausible numbers before local storage
// or terminal rendering.
func ValidateSample(s metrics.Sample, hostID string, now time.Time) error {
	if s.HostID != hostID || s.SiteID != "" || s.ObservedAt.IsZero() || len(s.Values) > len(allowedValues) || len(s.Unavailable) > 8 {
		return fmt.Errorf("collector sample rejected")
	}
	if s.ObservedAt.After(now.Add(time.Minute)) || s.ObservedAt.Before(now.Add(-HistoryWindow-time.Minute)) {
		return fmt.Errorf("collector sample timestamp rejected")
	}
	for key, value := range s.Values {
		if _, ok := allowedValues[key]; !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e18 {
			return fmt.Errorf("collector sample value rejected")
		}
		if (key == "cpu_percent" || key == "memory_percent" || key == "disk_percent") && value > 100 {
			return fmt.Errorf("collector sample percentage rejected")
		}
	}
	for _, reason := range s.Unavailable {
		if _, ok := allowedUnavailable[reason]; !ok {
			return fmt.Errorf("collector sample status rejected")
		}
	}
	return nil
}

func decodeRequest(data []byte, hostID string) (Request, error) {
	var request Request
	if len(data) == 0 || len(data) > 1024 {
		return request, fmt.Errorf("collector request rejected")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("collector request rejected")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Request{}, fmt.Errorf("collector request rejected")
	}
	if err := ValidateRequest(request, hostID); err != nil {
		return Request{}, err
	}
	return request, nil
}
