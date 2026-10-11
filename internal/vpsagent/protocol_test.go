package vpsagent

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func validSample(at time.Time) metrics.Sample {
	return metrics.Sample{
		HostID: "server-1", ObservedAt: at.UTC(),
		Values:      map[string]float64{"cpu_percent": 10, "memory_percent": 40, "disk_percent": 80},
		Unavailable: []string{},
	}
}

func TestDecodeRequestRequiresExactVersionAndHost(t *testing.T) {
	request := Request{Version: ProtocolVersion, HostID: "server-1"}
	data, _ := json.Marshal(request)
	if _, err := DecodeRequest(data, "server-1"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		[]byte(`{"version":1,"host_id":"other"}`),
		[]byte(`{"version":1,"host_id":"server-1","command":"exec"}`),
		[]byte(`{"version":1,"host_id":"server-1"}{}`),
		[]byte(`{"version":1,"host_id":"server-1"} trailing`),
		[]byte(`{"version":2,"host_id":"server-1"}`),
	} {
		if _, err := DecodeRequest(bad, "server-1"); err == nil {
			t.Fatalf("unsafe request accepted: %s", bad)
		}
	}
	if _, err := DecodeRequest(make([]byte, 1025), "server-1"); err == nil {
		t.Fatal("oversized request accepted")
	}
}

func TestValidateSampleRejectsUntrustedFields(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*metrics.Sample)
	}{
		{"wrong host", func(s *metrics.Sample) { s.HostID = "other" }},
		{"site payload", func(s *metrics.Sample) { s.SiteID = "site" }},
		{"unexpected metric", func(s *metrics.Sample) { s.Values["secret"] = 1 }},
		{"non-finite value", func(s *metrics.Sample) { s.Values["cpu_percent"] = math.NaN() }},
		{"percentage range", func(s *metrics.Sample) { s.Values["cpu_percent"] = 101 }},
		{"negative value", func(s *metrics.Sample) { s.Values["disk_free_bytes"] = -1 }},
		{"status injection", func(s *metrics.Sample) { s.Unavailable = []string{"\x1b[31m"} }},
		{"future timestamp", func(s *metrics.Sample) { s.ObservedAt = now.Add(2 * time.Minute) }},
		{"old timestamp", func(s *metrics.Sample) { s.ObservedAt = now.Add(-32 * time.Minute) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sample := validSample(now)
			test.mutate(&sample)
			if ValidateSample(sample, "server-1", now) == nil {
				t.Fatal("untrusted sample passed validation")
			}
		})
	}
}

func TestExpandedTelemetryIsOptionalAndRetainsNumericBounds(t *testing.T) {
	now := time.Now().UTC()
	sample := validSample(now)
	if err := ValidateSample(sample, "server-1", now); err != nil {
		t.Fatal("older minimal readings stopped working", err)
	}
	for key := range allowedValues {
		sample.Values[key] = 0
	}
	sample.Unavailable = []string{"pressure", "network"}
	if err := ValidateSample(sample, "server-1", now); err != nil {
		t.Fatal("bounded expanded telemetry was rejected", err)
	}
	sample.Values["pressure_io_some_percent"] = 101
	if err := ValidateSample(sample, "server-1", now); err == nil {
		t.Fatal("impossible pressure percentage accepted")
	}
}

func TestDecodeFrameOnlyAcceptsBoundedHistoryAndLiveMetrics(t *testing.T) {
	now := time.Now().UTC()
	history := Frame{Version: ProtocolVersion, HostID: "server-1", Kind: "history", Samples: []metrics.Sample{validSample(now)}}
	data, _ := json.Marshal(history)
	if _, err := DecodeFrame(data, "server-1", now); err != nil {
		t.Fatal(err)
	}
	live := Frame{Version: ProtocolVersion, HostID: "server-1", Kind: "host_metrics", Sample: ptrSample(validSample(now))}
	data, _ = json.Marshal(live)
	if _, err := DecodeFrame(data, "server-1", now); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		[]byte(`{"version":1,"host_id":"other","kind":"history","samples":[]}`),
		[]byte(`{"version":1,"host_id":"server-1","kind":"commands","samples":[]}`),
		[]byte(`{"version":1,"host_id":"server-1","kind":"host_metrics","sample":{"host_id":"server-1","site_id":"","observed_at":"2026-01-01T00:00:00Z","values":{"cpu_percent":1e999},"unavailable":[]}}`),
	} {
		if _, err := DecodeFrame(bad, "server-1", now); err == nil {
			t.Fatalf("unsafe frame accepted: %s", bad)
		}
	}
	tooMany := make([]metrics.Sample, MaxHistory+1)
	for i := range tooMany {
		tooMany[i] = validSample(now)
	}
	data, _ = json.Marshal(Frame{Version: ProtocolVersion, HostID: "server-1", Kind: "history", Samples: tooMany})
	if _, err := DecodeFrame(data, "server-1", now); err == nil {
		t.Fatal("oversized history count accepted")
	}
}

func TestRetainedKeepsOnlyRecentBoundedSamples(t *testing.T) {
	now := time.Now().UTC()
	samples := make([]metrics.Sample, MaxHistory+10)
	for i := range samples {
		samples[i] = validSample(now.Add(-time.Duration(len(samples)-i-1) * 10 * time.Second))
	}
	got := retained(samples, "server-1", now)
	if len(got) != MaxHistory {
		t.Fatalf("retained %d samples; want %d", len(got), MaxHistory)
	}
	if got[0].ObservedAt.Before(now.Add(-HistoryWindow)) {
		t.Fatal("old sample remained in rolling history")
	}
}

func TestHistoryCheckpointRoundTripsAndRejectsCorruption(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "history.json")
	want := []metrics.Sample{validSample(now.Add(-time.Minute)), validSample(now)}
	if err := saveHistory(path, "server-1", want, now); err != nil {
		t.Fatal(err)
	}
	got, err := loadHistory(path, "server-1", now)
	if err != nil || len(got) != len(want) {
		t.Fatalf("history round trip failed: %d samples, %v", len(got), err)
	}
	if _, err = loadHistory(path, "other", now); err == nil {
		t.Fatal("host-mismatched history accepted")
	}
	if err = os.WriteFile(path, []byte(`{"version":1,"host_id":"server-1","samples":[],"command":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadHistory(path, "server-1", now); err == nil {
		t.Fatal("unknown checkpoint field accepted")
	}
}

func TestHistoryCleanupRemovesOnlyStaleCheckpointFiles(t *testing.T) {
	dir := t.TempDir()
	checkpoint := filepath.Join(dir, ".hostops-history-interrupted.tmp")
	unrelated := filepath.Join(dir, "unrelated.tmp")
	for _, path := range []string{checkpoint, unrelated} {
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupHistoryTemps(filepath.Join(dir, "history.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatal("interrupted checkpoint was not removed")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file was removed", err)
	}
}

func ptrSample(sample metrics.Sample) *metrics.Sample { return &sample }
