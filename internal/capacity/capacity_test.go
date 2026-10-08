package capacity

import (
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestSparseHistoryDoesNotInventForecast(t *testing.T) {
	now := time.Now()
	r := Evaluate("h", []metrics.Sample{{ObservedAt: now, Values: map[string]float64{"disk_percent": 88, "disk_free_bytes": 4e9}}}, now)
	if r.State != model.Warn || r.CPU95 != nil || r.DaysTo85 != nil || !r.AlreadyAbove85 {
		t.Fatal(r)
	}
}

func TestEmptyOrStaleSamplesCannotPass(t *testing.T) {
	now := time.Now()
	for _, s := range []metrics.Sample{{ObservedAt: now, Values: map[string]float64{}}, {ObservedAt: now.Add(-time.Hour), Values: map[string]float64{"disk_percent": 20}}} {
		if Evaluate("h", []metrics.Sample{s}, now).State != model.Unknown {
			t.Fatal("missing evidence passed")
		}
	}
}
