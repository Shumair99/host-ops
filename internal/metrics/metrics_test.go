package metrics

import "testing"

func TestMetricUnitsAndCPUAccounting(t *testing.T) {
	for raw, want := range map[string]float64{"1GiB": 1073741824, "1GB": 1e9, "1.5MiB": 1572864, "0B": 0} {
		got, err := Bytes(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %f %v", raw, got, err)
		}
	}
	if _, err := Bytes("broken"); err == nil {
		t.Fatal("malformed metric accepted")
	}
	total, idle := cpu([]byte("cpu  10 2 3 80 5 0 0 0 2 0\n"))
	if total != 100 || idle != 85 {
		t.Fatal("guest CPU double-counted or iowait omitted")
	}
}
