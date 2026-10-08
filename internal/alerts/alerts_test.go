package alerts

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/incidents"
)

func TestWebhookRetriesOnlyTransientFailuresAndKeepsIdempotency(t *testing.T) {
	for _, code := range []int{204, 400, 302, 429, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			attempts := 0
			key, body := "", ""
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				data, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Idempotency-Key") == "" {
					t.Error("invalid delivery")
				}
				if attempts == 1 {
					key, body = r.Header.Get("Idempotency-Key"), string(data)
				} else if key != r.Header.Get("Idempotency-Key") || body != string(data) {
					t.Error("retry identity/body changed")
				}
				if code == 302 {
					w.Header().Set("Location", "https://example.invalid/secret")
				}
				if attempts == 1 {
					w.WriteHeader(code)
				} else {
					w.WriteHeader(204)
				}
			}))
			defer server.Close()
			err := notify(context.Background(), server.URL, incidents.Incident{ID: "outage", Status: "open"}, server.Client())
			want := 1
			if code == 429 || code == 503 {
				want = 2
			}
			if attempts != want || (err == nil) != (code == 204 || want == 2) {
				t.Fatal(attempts, err)
			}
		})
	}
}

func TestWebhookPermanentFailureIsBoundedAndDoesNotEchoSecrets(t *testing.T) {
	attempts := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(503)
		io.WriteString(w, "private-secret")
	}))
	defer s.Close()
	err := notify(context.Background(), s.URL+"/?token=private-secret", incidents.Incident{}, s.Client())
	if err == nil || attempts != 2 || strings.Contains(err.Error(), "private-secret") {
		t.Fatal(attempts, err)
	}
	for _, endpoint := range []string{"http://example.com", "https://user:password@example.com"} {
		if Notify(context.Background(), endpoint, incidents.Incident{}) == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if notify(ctx, s.URL, incidents.Incident{}, s.Client()) == nil {
		t.Fatal("cancelled delivery succeeded")
	}
}
