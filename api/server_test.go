package api

import (
	"net/http/httptest"
	"strings"
	"testing"

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
