package metrics

import (
	"errors"
	"testing"
)

func TestKernelExtrasPreservesMissingValuesAndInterfaceAccounting(t *testing.T) {
	files := map[string]string{
		"/proc/net/dev":         "Inter-| Receive | Transmit\n lo: 900 0 0 0 0 0 0 0 900 0 0 0 0 0 0 0\n eth0: 1200 0 0 0 0 0 0 0 350 0 0 0 0 0 0 0\n br0: 70 0 0 0 0 0 0 0 80 0 0 0 0 0 0 0\n",
		"/proc/pressure/cpu":    "some avg10=1.24 avg60=0.20 avg300=0.10 total=9\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0",
		"/proc/pressure/memory": "some avg10=NaN avg60=1 total=3\nfull avg10=104 avg60=2 total=5",
	}
	s := Sample{Values: map[string]float64{}}
	read := func(path string) ([]byte, error) {
		s, ok := files[path]
		if !ok {
			return nil, errors.New("not supported")
		}
		return []byte(s), nil
	}
	KernelExtras(&s, []byte("cpu 1 2 3 4 5\ncpu0 1 1 1 1\ncpu1 1 1 1 1\nprocs_running 3\nprocs_blocked 0"), []byte("MemAvailable: 128 kB\nCached: invalid kB\nSwapTotal: 0 kB"), []byte("0.20 0.15 0.12 3/184 912"), read)
	for key, want := range map[string]float64{"cpu_count": 2, "tasks_running": 3, "tasks_blocked": 0, "tasks_total": 184, "memory_available_bytes": 131072, "swap_total_bytes": 0, "network_rx_bytes": 1270, "network_tx_bytes": 430, "network_interfaces": 2, "pressure_cpu_some_percent": 1.24, "pressure_cpu_full_percent": 0} {
		if got, ok := s.Values[key]; !ok || got != want {
			t.Fatalf("%s got %v,%v want %v", key, got, ok, want)
		}
	}
	for _, key := range []string{"memory_cached_bytes", "pressure_memory_some_percent", "pressure_memory_full_percent", "pressure_io_some_percent"} {
		if _, ok := s.Values[key]; ok {
			t.Fatalf("missing or invalid %s became data", key)
		}
	}
	if n, ok := s.Values["network_rx_errors"]; !ok || n != 0 {
		t.Fatal("observed zero interface errors were lost")
	}
	if !hasReason(s.Unavailable, "pressure") {
		t.Fatal("unsupported pressure source was hidden")
	}
	identity := s.Values["network_interface_identity"]
	files["/proc/net/dev"] = " enp1: 1270 0 0 0 0 0 0 0 430 0 0 0 0 0 0 0\n br0: 70 0 0 0 0 0 0 0 80 0 0 0 0 0 0 0"
	next := Sample{Values: map[string]float64{}}
	KernelExtras(&next, nil, nil, nil, read)
	if next.Values["network_interface_identity"] == identity {
		t.Fatal("changed interface set retained its identity")
	}
}

func TestKernelExtrasUnavailableNetworkDoesNotBecomeZeroTraffic(t *testing.T) {
	s := Sample{Values: map[string]float64{}}
	KernelExtras(&s, nil, nil, nil, func(string) ([]byte, error) { return nil, errors.New("denied") })
	if _, ok := s.Values["network_rx_bytes"]; ok {
		t.Fatal("missing traffic became a zero counter")
	}
	if len(s.Unavailable) != 2 || !hasReason(s.Unavailable, "network") {
		t.Fatal("source failures were duplicated or hidden", s.Unavailable)
	}
}
