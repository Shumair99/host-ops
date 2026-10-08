package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestTimelineCombinesRecordKindsWithoutDumpingRawStructures(t *testing.T) {
	m := testDashboard(t)
	_, err := m.source.Store.Add(m.ctx, m.source.Config.Host.ID, "alpha", "change", time.Now(), map[string]string{"field": "runtime.image", "before": "old", "after": "new"})
	if err != nil {
		t.Fatal(err)
	}
	lines, err := m.source.Page(m.ctx, "events", "", "", m.snapshot)
	text := strings.Join(lines, "\n")
	if err != nil || !strings.Contains(text, "runtime.image") || !strings.Contains(text, "old -> new") || !strings.Contains(text, "Deployment:") || !strings.Contains(text, "scenario:") {
		t.Fatal("timeline omitted a record kind", err, text)
	}
	report := model.Report{Name: "domain.example", Overall: "healthy", Checks: []model.Check{{Layer: "tls", State: model.Pass}}}
	data, _ := json.Marshal(report)
	text = strings.Join(timelineLines("monitoring", []storage.Record{{Kind: "health", Data: data}}), "\n")
	if !strings.Contains(text, "domain.example [HEALTHY]") || strings.Contains(text, "CHECKS:") {
		t.Fatal("health history still dumps raw evidence", text)
	}
}
