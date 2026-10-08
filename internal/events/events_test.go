package events

import (
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
)

func TestMeaningfulEventsIgnoreLatencyNoise(t *testing.T) {
	before := model.Report{HostID: "h", SiteID: "s", Overall: "healthy", Runtime: model.Runtime{ImageID: "old"}, Checks: []model.Check{{Layer: "public_http", Summary: "HTTP 200; 12ms"}}}
	after := before
	after.Checks = []model.Check{{Layer: "public_http", Summary: "HTTP 200; 14ms"}}
	if len(Between(before, after)) != 0 {
		t.Fatal("latency noise generated an infrastructure event")
	}
	after.Runtime.ImageID = "new"
	ev := Between(before, after)
	if len(ev) != 1 || ev[0].Type != "image_changed" {
		t.Fatal(ev)
	}
}
