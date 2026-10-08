package api

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

func TestAPIAuthenticationAndLoopback(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("a", 32)
	handler := (&Server{Store: store, Config: config.Config{Host: config.Host{ID: "h"}}, Token: token}).Handler()
	for _, tc := range []struct {
		token, origin string
		want          int
	}{{"", "", 401}, {token, "", 200}, {token, "https://attacker.example", 403}} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:9090/api/status", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("got %d want %d", w.Code, tc.want)
		}
	}
	if Loopback("0.0.0.0:9090") || Loopback(":9090") || !Loopback("127.0.0.1:9090") {
		t.Fatal("unsafe listener allowed")
	}
}

func TestDeploymentNotificationIsIdempotentAndHostScoped(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("b", 32)
	handler := (&Server{Store: store, Config: config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "site"}}}, Token: token}).Handler()
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "http://127.0.0.1:9090/api/deployments", bytes.NewBufferString(`{"site_id":"site","id":"run-1","status":"completed","commit":"abcdef1","health_result":"healthy","host_id":"spoofed"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 && w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	records, err := store.Query(context.Background(), "h", "site", "deployment", time.Time{}, 100)
	if err != nil || len(records) != 1 {
		t.Fatal("duplicate deployment recorded", records, err)
	}
}
