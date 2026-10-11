package metrics

import (
	"hash/crc32"
	"math"
	"sort"
	"strconv"
	"strings"
)

// KernelExtras adds bounded, read-only counters to both Linux sampling paths.
// Network counters include every non-loopback interface: they are interface
// traffic, not a claim about unique internet traffic. No rates are fabricated.
func KernelExtras(s *Sample, stat, mem, load []byte, read func(string) ([]byte, error)) {
	parse := func(raw string) (float64, bool) {
		n, err := strconv.ParseFloat(raw, 64)
		return n, err == nil && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	cores := 0
	for _, line := range strings.Split(string(stat), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if strings.HasPrefix(f[0], "cpu") && len(f[0]) > 3 {
			if _, err := strconv.Atoi(f[0][3:]); err == nil {
				cores++
			}
		}
		key := map[string]string{"procs_running": "tasks_running", "procs_blocked": "tasks_blocked"}[f[0]]
		if key != "" {
			if n, ok := parse(f[1]); ok {
				s.Values[key] = n
			}
		}
	}
	if cores > 0 {
		s.Values["cpu_count"] = float64(cores)
	}
	for _, line := range strings.Split(string(mem), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[2] != "kB" {
			continue
		}
		key := map[string]string{"MemAvailable:": "memory_available_bytes", "Cached:": "memory_cached_bytes", "SwapTotal:": "swap_total_bytes"}[f[0]]
		if key != "" {
			if n, ok := parse(f[1]); ok {
				s.Values[key] = n * 1024
			}
		}
	}
	if f := strings.Fields(string(load)); len(f) >= 4 {
		if tasks := strings.Split(f[3], "/"); len(tasks) == 2 {
			if n, ok := parse(tasks[1]); ok {
				s.Values["tasks_total"] = n
			}
		}
	}
	for _, resource := range []string{"cpu", "memory", "io"} {
		data, err := read("/proc/pressure/" + resource)
		if err != nil {
			if !hasReason(s.Unavailable, "pressure") {
				s.Unavailable = append(s.Unavailable, "pressure")
			}
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || (f[0] != "some" && f[0] != "full") {
				continue
			}
			for _, field := range f[1:] {
				if strings.HasPrefix(field, "avg10=") {
					if n, ok := parse(strings.TrimPrefix(field, "avg10=")); ok && n <= 100 {
						s.Values["pressure_"+resource+"_"+f[0]+"_percent"] = n
					}
				}
			}
		}
	}
	data, err := read("/proc/net/dev")
	if err != nil {
		s.Unavailable = append(s.Unavailable, "network")
		return
	}
	rx, tx, interfaces := 0.0, 0.0, 0
	names := []string{}
	counters := map[string]float64{}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "lo" {
			continue
		}
		f := strings.Fields(parts[1])
		if len(f) < 16 {
			continue
		}
		r, rok := parse(f[0])
		t, tok := parse(f[8])
		if !rok || !tok {
			s.Unavailable = append(s.Unavailable, "network")
			return
		}
		for index, key := range map[int]string{1: "network_rx_packets", 2: "network_rx_errors", 3: "network_rx_dropped", 9: "network_tx_packets", 10: "network_tx_errors", 11: "network_tx_dropped"} {
			n, ok := parse(f[index])
			if !ok {
				s.Unavailable = append(s.Unavailable, "network")
				return
			}
			counters[key] += n
		}
		rx += r
		tx += t
		interfaces++
		names = append(names, strings.TrimSpace(parts[0]))
	}
	if interfaces > 0 {
		s.Values["network_rx_bytes"] = rx
		s.Values["network_tx_bytes"] = tx
		s.Values["network_interfaces"] = float64(interfaces)
		sort.Strings(names)
		s.Values["network_interface_identity"] = float64(crc32.ChecksumIEEE([]byte(strings.Join(names, "\x00"))))
		for key, n := range counters {
			s.Values[key] = n
		}
	}
}

func hasReason(reasons []string, reason string) bool {
	for _, s := range reasons {
		if s == reason {
			return true
		}
	}
	return false
}
