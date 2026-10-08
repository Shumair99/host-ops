//go:build linux

package vpsagent

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

type Sampler struct {
	hostID       string
	previousCPU  float64
	previousIdle float64
	hasCPU       bool
	diskAt       time.Time
	diskValues   map[string]float64
}

func NewSampler(hostID string) (*Sampler, error) {
	if !hostIDPattern.MatchString(hostID) {
		return nil, fmt.Errorf("invalid host id")
	}
	s := &Sampler{hostID: hostID, diskValues: map[string]float64{}}
	s.previousCPU, s.previousIdle, s.hasCPU = cpuCounters()
	return s, nil
}

func cpuCounters() (total, idle float64, ok bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		for i, raw := range fields[1:] {
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil || value < 0 || math.IsInf(value, 0) || math.IsNaN(value) {
				return 0, 0, false
			}
			if i < 8 {
				total += value
			}
			if i == 3 || i == 4 {
				idle += value
			}
		}
		return total, idle, true
	}
	return 0, 0, false
}

func (s *Sampler) Sample(ctx context.Context) metrics.Sample {
	observed := time.Now().UTC()
	sample := metrics.Sample{HostID: s.hostID, ObservedAt: observed, Values: map[string]float64{}, Unavailable: []string{}}
	select {
	case <-ctx.Done():
		return sample
	default:
	}
	if total, idle, ok := cpuCounters(); ok {
		if s.hasCPU && total > s.previousCPU {
			used := 100 * (1 - (idle-s.previousIdle)/(total-s.previousCPU))
			sample.Values["cpu_percent"] = min(100, max(0, used))
		} else {
			sample.Unavailable = append(sample.Unavailable, "cpu")
		}
		s.previousCPU, s.previousIdle, s.hasCPU = total, idle, true
	} else {
		sample.Unavailable = append(sample.Unavailable, "cpu")
		s.hasCPU = false
	}

	memData, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		sample.Unavailable = append(sample.Unavailable, "memory")
	} else {
		mem := map[string]float64{}
		for _, line := range strings.Split(string(memData), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, parseErr := strconv.ParseFloat(fields[1], 64)
			if parseErr == nil && value >= 0 && !math.IsInf(value, 0) && !math.IsNaN(value) {
				mem[strings.TrimSuffix(fields[0], ":")] = value * 1024
			}
		}
		if mem["MemTotal"] <= 0 || mem["MemAvailable"] < 0 || mem["MemAvailable"] > mem["MemTotal"] {
			sample.Unavailable = append(sample.Unavailable, "memory")
		} else {
			sample.Values["memory_total_bytes"] = mem["MemTotal"]
			sample.Values["memory_used_bytes"] = mem["MemTotal"] - mem["MemAvailable"]
			sample.Values["memory_percent"] = 100 * (1 - mem["MemAvailable"]/mem["MemTotal"])
			sample.Values["swap_used_bytes"] = max(0, mem["SwapTotal"]-mem["SwapFree"])
		}
	}

	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		if fields := strings.Fields(string(data)); len(fields) > 0 {
			if value, parseErr := strconv.ParseFloat(fields[0], 64); parseErr == nil && value >= 0 {
				sample.Values["uptime_seconds"] = value
			} else {
				sample.Unavailable = append(sample.Unavailable, "uptime")
			}
		} else {
			sample.Unavailable = append(sample.Unavailable, "uptime")
		}
	} else {
		sample.Unavailable = append(sample.Unavailable, "uptime")
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			for i, key := range []string{"load_1", "load_5", "load_15"} {
				if value, parseErr := strconv.ParseFloat(fields[i], 64); parseErr == nil && value >= 0 {
					sample.Values[key] = value
				} else {
					sample.Unavailable = append(sample.Unavailable, "load")
					break
				}
			}
		} else {
			sample.Unavailable = append(sample.Unavailable, "load")
		}
	} else {
		sample.Unavailable = append(sample.Unavailable, "load")
	}

	if s.diskAt.IsZero() || observed.Sub(s.diskAt) >= 30*time.Second {
		s.diskValues = rootDisk()
		if len(s.diskValues) > 0 {
			s.diskAt = observed
		}
	}
	if len(s.diskValues) == 0 || observed.Sub(s.diskAt) > time.Minute {
		sample.Unavailable = append(sample.Unavailable, "disk")
	} else {
		for key, value := range s.diskValues {
			sample.Values[key] = value
		}
	}
	return sample
}

func rootDisk() map[string]float64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil || stat.Bsize <= 0 || stat.Bfree > stat.Blocks || stat.Bavail > stat.Bfree {
		return nil
	}
	blockSize := float64(stat.Bsize)
	total := float64(stat.Blocks) * blockSize
	used := float64(stat.Blocks-stat.Bfree) * blockSize
	available := float64(stat.Bavail) * blockSize
	if total < 0 || used < 0 || available < 0 || used+available <= 0 {
		return nil
	}
	return map[string]float64{
		"disk_total_bytes": total, "disk_used_bytes": used,
		"disk_free_bytes": available, "disk_percent": 100 * used / (used + available),
		"disk_reserved_bytes": max(0, total-used-available),
	}
}
