package metrics

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// ParseStats is shared by session discovery and explicit manual inspection.
func ParseStats(data []byte) (map[string]float64, []string) {
	values := map[string]float64{}
	unavailable := []string{}
	var fields map[string]any
	if json.Unmarshal(data, &fields) == nil {
		for key, target := range map[string]string{"CPUPerc": "cpu_percent", "MemPerc": "memory_percent"} {
			if raw, ok := fields[key].(string); ok {
				n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(raw), "%"), 64)
				if err == nil && n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) && (target != "memory_percent" || n <= 100) {
					values[target] = n
				}
			}
		}
		for field, keys := range map[string][]string{"MemUsage": {"memory_used_bytes", "memory_limit_bytes"}, "NetIO": {"network_rx_bytes", "network_tx_bytes"}} {
			raw, _ := fields[field].(string)
			parts := strings.Split(raw, "/")
			if len(parts) != 2 {
				continue
			}
			for i, key := range keys {
				if n, err := Bytes(strings.TrimSpace(parts[i])); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) && n >= 0 {
					values[key] = n
				}
			}
		}
	}
	for _, key := range []string{"cpu_percent", "memory_percent", "memory_used_bytes", "memory_limit_bytes", "network_rx_bytes", "network_tx_bytes"} {
		if _, ok := values[key]; !ok {
			unavailable = append(unavailable, key+": runtime did not provide a usable value")
		}
	}
	return values, unavailable
}
