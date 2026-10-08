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
