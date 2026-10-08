package diagnostics

import (
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"testing"
	"time"
)

func TestCorrelationRequiresRegressionAndRecentDeployment(t *testing.T) {
	at := time.Now()
	before := model.Report{Overall: "healthy", Runtime: model.Runtime{ImageID: "old"}}
	after := model.Report{HostID: "h", SiteID: "s", Overall: "critical", ObservedAt: at, Runtime: model.Runtime{ImageID: "new"}}
	data := json.RawMessage(`{"status":"completed","commit":"abc"}`)
	records := []storage.Record{{ObservedAt: at.Add(-time.Minute), Data: data}}
	findings := Correlate(before, after, records)
	if len(findings) != 2 {
		t.Fatal(findings)
	}
	before.Overall = "critical"
	if len(Correlate(before, after, records)) != 0 {
		t.Fatal("repeated outage recounted as regression")
	}
	before.Overall = "healthy"
	before.Runtime.ImageID = "new"
	records[0].ObservedAt = at.Add(-time.Hour)
	if len(Correlate(before, after, records)) != 0 {
		t.Fatal("old deployment correlated")
	}
}
