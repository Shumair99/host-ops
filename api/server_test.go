package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
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

func TestWatcherValidationAndRetryDedup(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	token := strings.Repeat("x", 32)
	handler := (&Server{Store: store, Token: token, Config: config.Config{Host: config.Host{ID: "h"}, Sites: []config.Site{{ID: "s", HostID: "h", Health: config.Health{PublicURL: "https://s.example", ExpectedStatus: 200}}}}}).Handler()
	post := func(reports []model.Report) int {
		data, _ := json.Marshal(reports)
		req := httptest.NewRequest("POST", "http://127.0.0.1:9090/api/observations", bytes.NewReader(data))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	r := model.Report{SiteID: "s", ObservedAt: time.Now()}
	if post([]model.Report{r}) != 400 {
		t.Fatal("empty checks accepted")
	}
	r.Checks = []model.Check{{Layer: "dns", State: model.Pass}, {Layer: "tcp", State: model.Fail}, {Layer: "tls", State: model.Fail}, {Layer: "public_http", State: model.Fail}}
	for j := 0; j < 4; j++ {
		if post([]model.Report{r}) != 202 {
			t.Fatal("rejected valid batch")
		}
	}
	items, _ := store.Query(context.Background(), "h", "s", "external_health", time.Time{}, 100)
	if len(items) != 1 {
		t.Fatal("duplicate observations", items)
	}
	items, _ = store.Query(context.Background(), "h", "s", "incident", time.Time{}, 100)
	if len(items) != 0 {
		t.Fatal("retry opened incident")
	}
	for j := 0; j < 2; j++ {
		r.ObservedAt = r.ObservedAt.Add(time.Second)
		if post([]model.Report{r}) != 202 {
			t.Fatal("batch rejected")
		}
	}
	items, _ = store.Query(context.Background(), "h", "s", "incident", time.Time{}, 100)
	if len(items) != 1 {
		t.Fatal("new observations failed to open incident")
	}
	r.Checks[2].State = model.Skip
	if post([]model.Report{r}) != 400 {
		t.Fatal("HTTPS TLS skip accepted")
	}
	r.Checks[2].State = model.Pass
	r.Checks[3].State = model.Pass
	r.Checks[3].StatusCode = 503
	if post([]model.Report{r}) != 400 {
		t.Fatal("false HTTP pass accepted")
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1:9090/api/status", nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("raw token accepted")
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
