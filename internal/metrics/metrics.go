package metrics

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/platform"
)

type Sample struct {
	HostID      string             `json:"host_id"`
	SiteID      string             `json:"site_id"`
	ObservedAt  time.Time          `json:"observed_at"`
	Values      map[string]float64 `json:"values"`
	Unavailable []string           `json:"unavailable"`
}

func cpu(data []byte) (total, idle float64) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[0] == "cpu" {
			for i, value := range fields[1:] {
				n, _ := strconv.ParseFloat(value, 64)
				if i < 8 {
					total += n
				}
				if i == 3 || i == 4 {
					idle += n
				}
			}
			break
		}
	}
	return
}

func Host(ctx context.Context, runner platform.Runner, h config.Host) Sample {
	return hostSample(ctx, runner, h, true)
}

func HostFast(ctx context.Context, runner platform.Runner, h config.Host) Sample {
	return hostSample(ctx, runner, h, false)
}

func hostSample(ctx context.Context, runner platform.Runner, h config.Host, disk bool) Sample {
	s := Sample{HostID: h.ID, ObservedAt: time.Now().UTC(), Values: map[string]float64{}, Unavailable: []string{}}
	get := func(name, path string) []byte {
		data, err := runner.Run(ctx, "cat", path)
		if err != nil {
			s.Unavailable = append(s.Unavailable, name)
			return nil
		}
		return data
	}
	before := get("cpu", "/proc/stat")
	timer := time.NewTimer(200 * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
		return s
	case <-timer.C:
	}
	after := get("cpu", "/proc/stat")
	a, ai := cpu(before)
	b, bi := cpu(after)
	if b > a {
		s.Values["cpu_percent"] = 100 * (1 - (bi-ai)/(b-a))
	}
	mem := map[string]float64{}
	memData := get("memory", "/proc/meminfo")
	for _, line := range strings.Split(string(memData), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			n, _ := strconv.ParseFloat(f[1], 64)
			mem[strings.TrimSuffix(f[0], ":")] = n * 1024
		}
	}
	if mem["MemTotal"] > 0 {
		s.Values["memory_total_bytes"] = mem["MemTotal"]
		s.Values["memory_used_bytes"] = mem["MemTotal"] - mem["MemAvailable"]
		s.Values["memory_percent"] = 100 * (1 - mem["MemAvailable"]/mem["MemTotal"])
		s.Values["swap_used_bytes"] = mem["SwapTotal"] - mem["SwapFree"]
	}
	if fields := strings.Fields(string(get("uptime", "/proc/uptime"))); len(fields) > 0 {
		s.Values["uptime_seconds"], _ = strconv.ParseFloat(fields[0], 64)
	}
	loadData := get("load", "/proc/loadavg")
	if fields := strings.Fields(string(loadData)); len(fields) >= 3 {
		s.Values["load_1"], _ = strconv.ParseFloat(fields[0], 64)
		s.Values["load_5"], _ = strconv.ParseFloat(fields[1], 64)
		s.Values["load_15"], _ = strconv.ParseFloat(fields[2], 64)
	}
	KernelExtras(&s, after, memData, loadData, func(path string) ([]byte, error) { return runner.Run(ctx, "cat", path) })
	if !disk {
		return s
	}
	for key, value := range RootDisk(ctx, runner) {
		s.Values[key] = value
	}
	if _, ok := s.Values["disk_percent"]; !ok {
		s.Unavailable = append(s.Unavailable, "disk")
	}
	return s
}

func RootDisk(ctx context.Context, runner platform.Runner) map[string]float64 {
	s := Sample{Values: map[string]float64{}}
	data, err := runner.Run(ctx, "df", "-Pk", "/")
	if err != nil {
		s.Unavailable = append(s.Unavailable, "disk")
	} else {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 1 {
			f := strings.Fields(lines[len(lines)-1])
			if len(f) >= 6 {
				total, _ := strconv.ParseFloat(f[1], 64)
				used, _ := strconv.ParseFloat(f[2], 64)
				free, _ := strconv.ParseFloat(f[3], 64)
				s.Values["disk_total_bytes"] = total * 1024
				s.Values["disk_used_bytes"] = used * 1024
				s.Values["disk_free_bytes"] = free * 1024
				if used+free > 0 {
					// Match df: filesystem-reserved blocks are not available to the operator.
					s.Values["disk_percent"] = 100 * used / (used + free)
					s.Values["disk_reserved_bytes"] = (total - used - free) * 1024
				}
			}
		}
	}
	return s.Values
}

func Bytes(raw string) (float64, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty size")
	}
	raw = fields[0]
	i := 0
	for i < len(raw) && (raw[i] >= '0' && raw[i] <= '9' || raw[i] == '.') {
		i++
	}
	n, err := strconv.ParseFloat(raw[:i], 64)
	if err != nil {
		return 0, err
	}
	units := map[string]float64{"B": 1, "kB": 1000, "KB": 1000, "MB": 1e6, "GB": 1e9, "TB": 1e12, "KiB": 1024, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}
	factor, ok := units[raw[i:]]
	if !ok {
		return 0, fmt.Errorf("unknown size unit")
	}
	return n * factor, nil
}

func Container(ctx context.Context, runner platform.Runner, s config.Site) Sample {
	m := Sample{HostID: s.HostID, SiteID: s.ID, ObservedAt: time.Now().UTC(), Values: map[string]float64{}, Unavailable: []string{}}
	if s.Runtime.Type != "docker" && s.Runtime.Type != "podman" {
		m.Unavailable = append(m.Unavailable, "container metrics do not apply to this runtime")
		return m
	}
	data, err := runner.Run(ctx, s.Runtime.Type, "stats", "--no-stream", "--format", "{{json .}}", "--", s.Runtime.Container)
	if err != nil {
		m.Unavailable = append(m.Unavailable, "container stats")
		return m
	}
	m.Values, m.Unavailable = ParseStats(data)
	return m
}
