package health

import (
	"context"
	"errors"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAutomaticHTTPSeparatesReachabilityFromApplicationHealth(t *testing.T) {
	for _, tc := range []struct {
		code  int
		state model.State
	}{{200, model.Pass}, {401, model.Warn}, {404, model.Warn}, {503, model.Fail}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code) }))
		s := config.Site{ID: "s", HostID: "h", Health: config.Health{Automatic: true, TimeoutSeconds: 2}}
		check := HTTP(context.Background(), s, server.URL, "public_http")
		server.Close()
		if check.State != tc.state || check.StatusCode != tc.code {
			t.Fatal(check)
		}
	}
}
func TestRedirectLoopAndCredentialRedirectFailWithoutLeakingSecrets(t *testing.T) {
	for _, location := range []string{"/loop", "http://user:private-secret@127.0.0.1/"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, location, 302) }))
		check := HTTP(context.Background(), config.Site{Health: config.Health{TimeoutSeconds: 2, ExpectedStatus: 200}}, server.URL, "public_http")
		server.Close()
		if check.State != model.Fail || strings.Contains(check.Summary, "private-secret") {
			t.Fatal(check)
		}
	}
}
func TestNetworkUsesLocalFixtureAndValidatesExpectedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	s := config.Site{ID: "s", HostID: "h", Health: config.Health{TimeoutSeconds: 2, PublicURL: server.URL}, Proxy: config.Proxy{ExpectedIPs: []string{"192.0.2.1"}}}
	checks := Network(context.Background(), s)
	if checks[0].State != model.Fail || checks[1].State != model.Pass || checks[2].State != model.Skip {
		t.Fatal(checks)
	}
	s.Proxy.ExpectedIPs = []string{"127.0.0.1"}
	if Network(context.Background(), s)[0].State != model.Pass {
		t.Fatal("literal loopback DNS rejected")
	}
}

type localRunner struct {
	data     string
	fail     bool
	endpoint string
}

func (r *localRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.endpoint = args[len(args)-1]
	if r.fail {
		return nil, errors.New("secret")
	}
	return []byte(r.data), nil
}
func TestLocalProbeUsesObservedNetworkAndKeepsUnavailableUnknown(t *testing.T) {
	s := config.Site{Runtime: config.Runtime{Type: "docker", Network: "web", Port: 8080}, Health: config.Health{Path: "/ready", ExpectedStatus: 200, TimeoutSeconds: 2}}
	runtime := model.Runtime{IPs: map[string]string{"web": "::1"}}
	runner := &localRunner{data: "200 0.025"}
	check := LocalHTTP(context.Background(), runner, s, runtime)
	if check.State != model.Pass || check.LatencyMS != 25 || runner.endpoint != "http://[::1]:8080/ready" {
		t.Fatal(check, runner.endpoint)
	}
	runner.fail = true
	if LocalHTTP(context.Background(), runner, s, runtime).State != model.Unknown {
		t.Fatal("inspection failure claimed outage")
	}
	runner.fail = false
	runner.data = "invalid"
	if LocalHTTP(context.Background(), runner, s, runtime).State != model.Unknown {
		t.Fatal("malformed output accepted")
	}
}
