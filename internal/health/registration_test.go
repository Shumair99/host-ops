package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
)

type registryTransport func(*http.Request) (*http.Response, error)

func (f registryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func registryReply(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/rdap+json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

func TestRegistryLifecycleUsesMatchingTopLevelEvidenceAndDoesNotRetainContacts(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct{ status, date, name, code string }{
		{"client hold", now.Add(time.Hour).Format(time.RFC3339), "example.com", "registration_hold"},
		{"redemption period", now.Add(time.Hour).Format(time.RFC3339), "example.com", "registration_redemption"},
		{"pending delete", now.Add(time.Hour).Format(time.RFC3339), "example.com", "registration_pending_delete"},
		{"auto renew period", now.Add(time.Hour).Format(time.RFC3339), "example.com", "registration_auto_renew"},
		{"active", now.Add(-time.Hour).Format(time.RFC3339), "example.com", "registration_expiry_elapsed"},
		{"active", "invalid", "example.com", "registration_record"},
		{"active", now.Add(-time.Hour).Format(time.RFC3339), "other.com", "registration_unavailable"},
	} {
		body := fmt.Sprintf(`{"objectClassName":"domain","ldhName":%q,"status":[%q,"private-contact-secret"],"events":[{"eventAction":"expiration","eventDate":%q}],"entities":[{"secret":"private-contact-secret","events":[{"eventAction":"expiration","eventDate":"2000-01-01T00:00:00Z"}]}]}`, tc.name, tc.status, tc.date)
		check := registrationResponse([]byte(body), "example.com", "https://rdap.registry.test/domain/example.com", now)
		if check.Code != tc.code || strings.Contains(fmt.Sprint(check), "private-contact-secret") || strings.Contains(check.Summary, "ownership expired") {
			t.Fatal(tc, check)
		}
	}
	conflicting := `{"objectClassName":"domain","ldhName":"example.com","events":[{"eventAction":"expiration","eventDate":"2000-01-01T00:00:00Z"},{"eventAction":"expiration","eventDate":"2099-01-01T00:00:00Z"}]}`
	if c := registrationResponse([]byte(conflicting), "example.com", "source", now); c.Code != "registration_record" {
		t.Fatal("ambiguous dates claimed an expiry", c)
	}
}

func TestRegistrationDomainAndDestinationBoundaries(t *testing.T) {
	for host, want := range map[string]string{"www.example.com": "example.com", "app.example.co.uk": "example.co.uk", "EXAMPLE.COM.": "example.com", "127.0.0.1": "", "localhost": "", "foo.github.io": "", "co.uk": "", "bad.example.invalid": "", "bad/host.com": ""} {
		got, ok := registrationDomain(host)
		if got != want || ok != (want != "") {
			t.Fatal(host, got, ok)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "192.168.1.1", "100.64.0.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "224.0.0.1"} {
		if publicRegistryIP(netip.MustParseAddr(ip)) {
			t.Fatal("private/special destination accepted", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicRegistryIP(netip.MustParseAddr(ip)) {
			t.Fatal("public IP rejected", ip)
		}
	}
	for _, endpoint := range []string{"http://rdap.registry.test/", "https://user:secret@rdap.registry.test/", "https://rdap.registry.test/?token=secret", "https://rdap.registry.test:8443/", "https://127.0.0.1/", "https://localhost/"} {
		if registryURL(endpoint) {
			t.Fatal("unsafe registry URL accepted", endpoint)
		}
	}
	if _, err := publicRegistryDial(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("dial did not reject loopback")
	}
}

func TestRegistryQueriesAreCachedDeduplicatedAndUnauthenticated(t *testing.T) {
	r := newRegistrationClient()
	var calls atomic.Int32
	r.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.Method != "GET" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.URL.User != nil || req.URL.Scheme != "https" {
			t.Error("credentialed or unsafe registry request")
		}
		if req.URL.String() == rdapBootstrap {
			return registryReply(req, 200, `{"services":[[["com"],["http://insecure.registry.test/","https://rdap.registry.test/"]]]}`), nil
		}
		if req.URL.String() != "https://rdap.registry.test/domain/example.com" {
			t.Error("unexpected registry request", req.URL)
		}
		return registryReply(req, 200, `{"objectClassName":"domain","ldhName":"example.com","status":["client hold"]}`), nil
	})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c := r.lookup(context.Background(), "example.com"); c.Code != "registration_hold" {
				t.Error(c)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 2 {
		t.Fatal("registry lookup was not coalesced/cached", calls.Load())
	}
	if r.lookup(context.Background(), "example.com").Code != "registration_hold" || calls.Load() != 2 {
		t.Fatal("cached result lost")
	}
	// Healthy endpoints and private/local domains never trigger enrichment.
	for _, s := range []config.Site{{Health: config.Health{PublicURL: "https://example.com"}}, {Health: config.Health{PublicURL: "https://localhost"}}} {
		checks := []model.Check{{Layer: "public_http", State: model.Pass}}
		if len(EnrichRegistration(context.Background(), s, checks)) != 1 {
			t.Fatal("healthy probe unexpectedly enriched")
		}
	}
}

func TestRegistryUnavailableResponsesNeverClaimARegistrationFault(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{404, `{"errorCode":404}`, "registration_not_found"},
		{404, `{"message":"arbitrary not found"}`, "registration_unavailable"},
		{429, `{"errorCode":429}`, "registration_unavailable"},
		{200, strings.Repeat(" ", (1<<20)+1), "registration_unavailable"},
	} {
		r := newRegistrationClient()
		r.services, r.bootstrapUntil = map[string]string{"com": "https://rdap.registry.test/"}, time.Now().Add(time.Hour)
		r.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) { return registryReply(req, tc.status, tc.body), nil })
		c := r.lookup(context.Background(), "example.com")
		if c.Code != tc.code || c.Code == "registration_unavailable" && c.State != model.Skip {
			t.Fatal(tc.code, c)
		}
	}
	r := newRegistrationClient()
	var calls atomic.Int32
	r.client.Transport = registryTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		res := registryReply(req, 302, "")
		res.Header.Set("Location", "https://127.0.0.1/")
		return res, nil
	})
	if c := r.lookup(context.Background(), "example.com"); c.State != model.Skip || calls.Load() != 1 {
		t.Fatal("registry redirect followed", c, calls.Load())
	}
}

func TestLiveRegistrationLookup(t *testing.T) {
	domain := os.Getenv("HOSTOPS_LIVE_RDAP_DOMAIN")
	if domain == "" {
		t.Skip("explicitly set HOSTOPS_LIVE_RDAP_DOMAIN for public-registry integration verification")
	}
	domain, ok := registrationDomain(domain)
	if !ok {
		t.Fatal("integration domain must be a public registrable name")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := newRegistrationClient().lookup(ctx, domain)
	if c.State == model.Skip {
		t.Fatal(c.Summary)
	}
	t.Log(c.Code, c.Summary, c.Details)
}
