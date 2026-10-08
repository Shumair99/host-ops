package evidence

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"strings"
	"testing"
	"time"
)

func TestDeploymentRetryIsIdempotentAndHostScoped(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "s"}}}
	data := `{"site_id":"s","id":"run-1","status":"completed","commit":"abcdef1","host_id":"spoofed"}`
	for j := 0; j < 2; j++ {
		count, err := Record(context.Background(), c, s, "deployment", strings.NewReader(data))
		if err != nil || count != 1-j {
			t.Fatal(count, err)
		}
	}
	r, err := s.Query(context.Background(), "h", "s", "deployment", time.Time{}, 100)
	if err != nil || len(r) != 1 {
		t.Fatal(r, err)
	}
}
func TestObservationValidationAndDedup(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "s", Health: config.Health{PublicURL: "https://s.example", ExpectedStatus: 200}}}}
	ctx := context.Background()
	now := time.Now()
	r := model.Report{SiteID: "s", ObservedAt: now}
	if _, err = Observe(ctx, c, s, []model.Report{r}, now); err == nil {
		t.Fatal("empty checks accepted")
	}
	r.Checks = []model.Check{{Layer: "dns", State: model.Pass}, {Layer: "tcp", State: model.Fail}, {Layer: "tls", State: model.Fail}, {Layer: "public_http", State: model.Fail}}
	for j := 0; j < 4; j++ {
		_, err = Observe(ctx, c, s, []model.Report{r}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	records, _ := s.Query(ctx, "h", "s", "incident", time.Time{}, 100)
	if len(records) != 0 {
		t.Fatal("retry opened incident")
	}
	for j := 0; j < 2; j++ {
		r.ObservedAt = r.ObservedAt.Add(time.Second)
		if _, err = Observe(ctx, c, s, []model.Report{r}, now); err != nil {
			t.Fatal(err)
		}
	}
	records, _ = s.Query(ctx, "h", "s", "incident", time.Time{}, 100)
	if len(records) != 1 {
		t.Fatal("fresh observations did not open incident")
	}
	r.Checks[2].State = model.Skip
	if _, err = Observe(ctx, c, s, []model.Report{r}, now); err == nil {
		t.Fatal("HTTPS TLS skip accepted")
	}
	r.Checks[2].State = model.Pass
	r.Checks[3].State = model.Pass
	r.Checks[3].StatusCode = 503
	if _, err = Observe(ctx, c, s, []model.Report{r}, now); err == nil {
		t.Fatal("false HTTP pass accepted")
	}
}
func TestStrictDecodeRejectsTrailingUnknownAndOversized(t *testing.T) {
	for _, data := range []string{`{"unknown":true}`, `{} {}`, strings.Repeat(" ", 256*1024+1)} {
		var report model.Report
		if Decode(strings.NewReader(data), &report) == nil {
			t.Fatal("unsafe input accepted")
		}
	}
}
func TestCLIRecoveryEvidencePreservesReportedOrigin(t *testing.T) {
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "s"}}}
	at := time.Now().Add(-time.Minute).UTC()
	data, _ := json.Marshal(map[string]any{"site_id": "s", "id": "backup-one", "status": "succeeded", "size_bytes": 1024, "completed_at": at, "destination": "offsite"})
	if _, err = Record(context.Background(), c, s, "backup", strings.NewReader(string(data))); err != nil {
		t.Fatal(err)
	}
	records, _ := s.Query(context.Background(), "h", "s", "backup", time.Time{}, 1)
	if len(records) != 1 || !strings.Contains(string(records[0].Data), "operator_report") {
		t.Fatal(records)
	}
}
