package incidents

import (
	"context"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestThresholdsUnknownAndRecovery(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	r := model.Report{HostID: "h", SiteID: "s", Overall: "critical", ObservedAt: time.Now()}
	for i := 0; i < 2; i++ {
		ev, err := Observe(ctx, store, r, "watcher", 3, 2)
		if err != nil || ev != nil {
			t.Fatal("opened too early", ev, err)
		}
	}
	ev, err := Observe(ctx, store, r, "watcher", 3, 2)
	if err != nil || ev == nil || ev.Status != "open" {
		t.Fatal("did not open", ev, err)
	}
	r.Overall = "unknown"
	ev, _ = Observe(ctx, store, r, "watcher", 3, 2)
	if ev != nil {
		t.Fatal("unknown closed incident")
	}
	r.Overall = "healthy"
	ev, _ = Observe(ctx, store, r, "watcher", 3, 2)
	if ev != nil {
		t.Fatal("recovered too early")
	}
	ev, _ = Observe(ctx, store, r, "watcher", 3, 2)
	if ev == nil || ev.Status != "resolved" || ev.ResolvedAt == nil {
		t.Fatal("did not recover", ev)
	}
}
