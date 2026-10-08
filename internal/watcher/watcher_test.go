package watcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestWatcherPersistsThresholdsAndFinalFailedDelivery(t *testing.T) {
	status := 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer server.Close()
	s, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "s", HostID: "h", Health: config.Health{PublicURL: server.URL, ExpectedStatus: 200, TimeoutSeconds: 1}}}}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := Cycle(ctx, c, s, 2, 2, "http://invalid-endpoint"); err != nil {
			t.Fatal(err)
		}
		alerts, err := s.Query(ctx, "h", "s", "alert", time.Time{}, 100)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if i == 0 {
			want = 0
		}
		if len(alerts) != want {
			t.Fatal("threshold or duplicate notification", i, len(alerts))
		}
	}
	status = 200
	for i := 0; i < 2; i++ {
		if _, err := Cycle(ctx, c, s, 2, 2, "http://invalid-endpoint"); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.Query(ctx, "h", "s", "alert", time.Time{}, 100)
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	var delivery map[string]string
	if json.Unmarshal(records[0].Data, &delivery) != nil || delivery["delivery"] != "failed" || delivery["status"] != "resolved" || delivery["issue"] == "" {
		t.Fatal(string(records[0].Data))
	}
}
