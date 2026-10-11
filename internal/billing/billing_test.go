package billing

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func TestEnteredTokenStaysInSessionAndIsScopedToItsProvider(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})
	t.Setenv("HOSTOPS_HETZNER_TOKEN", "environment-token")
	t.Setenv("HOSTOPS_HOSTINGER_TOKEN", "other-provider-token")
	ctx := WithSessionToken(context.Background(), "hetzner", "", "fixture-token")
	var response map[string]any
	if err := client.get(ctx, "hetzner", "/servers", "", &response); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HOSTOPS_HETZNER_TOKEN") != "environment-token" || tokenValue(context.Background(), "hetzner", "") != "environment-token" || tokenValue(ctx, "hostinger", "") != "other-provider-token" {
		t.Fatal("session token escaped its provider/context or changed environment")
	}
	if tokenValue(ctx, "hetzner", "ALTERNATE_TOKEN") == "fixture-token" {
		t.Fatal("session token escaped its named credential scope")
	}
}

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	t.Setenv("HOSTOPS_HETZNER_TOKEN", "fixture-token")
	t.Setenv("HOSTOPS_HOSTINGER_TOKEN", "fixture-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("provider operation was not an authenticated GET")
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	base, _ := url.Parse(server.URL)
	client := NewClient()
	client.HTTP.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host = base.Scheme, base.Host
		return http.DefaultTransport.RoundTrip(copy)
	})
	return client
}

func TestHetznerAllocatedLifetimeCapAndBackups(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/servers/42":
			fmt.Fprint(w, `{"server":{"id":42,"created":"2026-10-08T01:01:00Z","server_type":{"name":"test-arm"},"datacenter":{"location":{"name":"fsn1"}},"backup_window":"22-02","status":"off"}}`)
		case "/v1/pricing":
			fmt.Fprint(w, `{"pricing":{"currency":"EUR","server_backup":{"percentage":"20"},"server_types":[{"name":"test-arm","prices":[{"location":"fsn1","price_hourly":{"gross":"0.1"},"price_monthly":{"gross":"10"}}]}]}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	now := time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC)
	r, err := client.Observe(context.Background(), config.Billing{Provider: "hetzner", ResourceID: "42"}, now)
	if err != nil || r.MonthlyEstimate == nil || *r.MonthlyEstimate != 12 || r.ServerMonthToDate == nil || *r.ServerMonthToDate != .2 {
		t.Fatal("server lifetime or enabled backups priced incorrectly", err, r)
	}
	r, err = client.Observe(context.Background(), config.Billing{Provider: "hetzner", ResourceID: "42"}, now.Add(15*24*time.Hour))
	if err != nil || *r.ServerMonthToDate != 10 {
		t.Fatal("month-to-date exceeded its monthly cap", err, r)
	}
}

func TestHostingerCurrentPriceAndRenewalRemainDistinct(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/v1/subscriptions" {
			t.Error("unexpected endpoint")
		}
		fmt.Fprint(w, `[{"id":"selected","name":"KVM test","status":"active","billing_period":12,"billing_period_unit":"month","currency_code":"GBP","total_price":12000,"renewal_price":24000},{"id":"other","name":"other","status":"active","billing_period":1,"billing_period_unit":"month","currency_code":"GBP","total_price":99999}]`)
	})
	r, err := client.Observe(context.Background(), config.Billing{Provider: "hostinger", ResourceID: "selected"}, time.Now())
	if err != nil || r.MonthlyEstimate == nil || *r.MonthlyEstimate != 10 || !strings.Contains(strings.Join(r.Notes, " "), "240.00 GBP") {
		t.Fatal("introductory/renewal or selected subscription was confused", err, r)
	}
	if _, err = client.Observe(context.Background(), config.Billing{Provider: "hostinger", ResourceID: "missing"}, time.Now()); err == nil {
		t.Fatal("unknown subscription selected another account resource")
	}
}

func TestMissingTokensAndProviderErrorsCannotExposeSecrets(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "fixture-token private account payload")
	})
	_, err := client.Resources(context.Background(), "hostinger", "")
	if err == nil || strings.Contains(err.Error(), "fixture-token") || !strings.Contains(err.Error(), "401") {
		t.Fatal("provider body leaked or failure was concealed", err)
	}
	t.Setenv("HOSTOPS_HOSTINGER_TOKEN", "")
	_, err = client.Resources(context.Background(), "hostinger", "")
	if err == nil || !strings.Contains(err.Error(), "HOSTOPS_HOSTINGER_TOKEN") {
		t.Fatal("missing credential did not explain setup", err)
	}
}

func TestMissingProviderAmountCannotBecomeFreeHosting(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":"selected","name":"KVM test","status":"active","billing_period":1,"billing_period_unit":"month","currency_code":"GBP"}]`)
	})
	if _, err := client.Observe(context.Background(), config.Billing{Provider: "hostinger", ResourceID: "selected"}, time.Now()); err == nil {
		t.Fatal("missing provider amount became a known zero budget")
	}
}

func TestProviderRedirectsAreNotFollowed(t *testing.T) {
	requests := 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "https://unexpected.invalid/token", http.StatusFound)
	})
	_, err := client.Resources(context.Background(), "hetzner", "")
	if err == nil || requests != 1 {
		t.Fatal("provider redirect was followed", err, requests)
	}
}

func TestCachedPricingMustMatchSelectedResourceAndManualZeroIsKnown(t *testing.T) {
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	amount := 12.0
	now := time.Now().UTC()
	cached := Report{Provider: "hetzner", ResourceID: "42", Currency: "EUR", ObservedAt: now, MonthlyEstimate: &amount}
	if err = db.Transition(ctx, "h", "", "billing", cached, "", now, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOSTOPS_HETZNER_TOKEN", "")
	h := config.Host{ID: "h", Billing: &config.Billing{Provider: "hetzner", ResourceID: "42"}}
	r, err := Resolve(ctx, h, db, false)
	if err != nil || *r.MonthlyEstimate != 12 {
		t.Fatal("matching cached pricing was not used", err, r)
	}
	h.Billing.ResourceID = "43"
	if _, err = Resolve(ctx, h, db, false); err == nil {
		t.Fatal("pricing for the wrong resource was reused")
	}
	zero := 0.0
	h.Billing = &config.Billing{Provider: "self-hosted", MonthlyAmount: &zero}
	r, err = Resolve(ctx, h, db, false)
	if err != nil || r.MonthlyEstimate == nil || *r.MonthlyEstimate != 0 {
		t.Fatal("explicit zero budget became unknown", err, r)
	}
	h.Billing.MonthlyAmount = nil
	r, err = Resolve(ctx, h, db, false)
	if err != nil || r.MonthlyEstimate != nil {
		t.Fatal("unbudgeted own hardware was assigned a price", err, r)
	}
}
