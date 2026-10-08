package metrics

import (
	"math"
	"testing"
)

func TestStatsParserPreservesRealZeroAndRejectsMalformedFields(t *testing.T) {
	values, missing := ParseStats([]byte(`{"CPUPerc":"0.00%","MemPerc":"38.5%","MemUsage":"1.4GiB / 3.7GiB","NetIO":"10kB / 2MB"}`))
	if len(missing) != 0 || values["cpu_percent"] != 0 || math.Abs(values["memory_used_bytes"]-1.4*1024*1024*1024) > 1 {
		t.Fatal(values, missing)
	}
	for _, data := range []string{`{}`, `invalid`, `{"CPUPerc":"NaN%","MemPerc":"101%","MemUsage":"-1B / unknown","NetIO":12}`, `{"CPUPerc":12,"MemPerc":null}`} {
		values, missing = ParseStats([]byte(data))
		if len(values) != 0 || len(missing) != 6 {
			t.Fatal("invalid metrics represented as data", data, values, missing)
		}
	}
	values, _ = ParseStats([]byte(`{"CPUPerc":"250%"}`))
	if values["cpu_percent"] != 250 {
		t.Fatal("multicore container CPU rejected")
	}
}
