package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

func TestHTTPAndTLSDoNotAcceptFalseHealth(t *testing.T) {
	s := config.Site{ID: "s", HostID: "h", Health: config.Health{TimeoutSeconds: 2, ExpectedStatus: 200}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	defer server.Close()
	check := HTTP(context.Background(), s, server.URL, "public_http")
	if check.State != model.Fail || check.StatusCode != 502 {
		t.Fatal(check)
	}
	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer untrusted.Close()
	if HTTP(context.Background(), s, untrusted.URL, "public_http").State != model.Fail {
		t.Fatal("untrusted TLS accepted")
	}
}
