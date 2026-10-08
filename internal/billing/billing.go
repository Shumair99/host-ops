// Package billing normalizes explicit billing sources into local evidence.
// Provider integrations issue GET requests only; machine resource usage is not
// evidence of a provider's prices, discounts or final invoice.
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/storage"
)

type Report struct {
	Provider          string    `json:"provider"`
	ResourceID        string    `json:"resource_id,omitempty"`
	Plan              string    `json:"plan,omitempty"`
	Source            string    `json:"source"`
	Currency          string    `json:"currency,omitempty"`
	ObservedAt        time.Time `json:"observed_at"`
	MonthlyEstimate   *float64  `json:"monthly_estimate,omitempty"`
	ServerMonthlyCap  *float64  `json:"server_monthly_cap,omitempty"`
	HourlyRate        *float64  `json:"server_hourly_rate,omitempty"`
	ServerMonthToDate *float64  `json:"server_month_to_date_estimate,omitempty"`
	Notes             []string  `json:"notes"`
}

type Resource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Plan     string `json:"plan"`
	Location string `json:"location,omitempty"`
}

type Client struct{ HTTP *http.Client }

func NewClient() Client {
	return Client{HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func TokenName(provider, override string) string {
	if override != "" {
		return override
	}
	return "HOSTOPS_" + strings.ToUpper(provider) + "_TOKEN"
}

func (c Client) get(ctx context.Context, provider, path, env string, out any) error {
	base := map[string]string{"hetzner": "https://api.hetzner.cloud/v1", "hostinger": "https://developers.hostinger.com/api"}[provider]
	if base == "" {
		return fmt.Errorf("unsupported billing API")
	}
	token := strings.TrimSpace(os.Getenv(TokenName(provider, env)))
	if token == "" {
		return fmt.Errorf("set %s in this terminal to read provider billing", TokenName(provider, env))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return fmt.Errorf("invalid billing request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s billing API unavailable", provider)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s billing API returned HTTP %d", provider, response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(out); err != nil {
		return fmt.Errorf("invalid %s billing response", provider)
	}
	return nil
}

func validMoney(value float64) bool { return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) }

func Resolve(ctx context.Context, host config.Host, store *storage.Store, force bool) (Report, error) {
	now := time.Now().UTC()
	b := host.Billing
	if b == nil {
		r := Report{Provider: "manual", Currency: host.Currency, ObservedAt: now, Source: "configured monthly budget", Notes: []string{"This is a budget, not an imported invoice."}}
		if host.MonthlyCost+host.MonthlyBackupCost > 0 {
			total := host.MonthlyCost + host.MonthlyBackupCost
			r.MonthlyEstimate = &total
		}
		return r, nil
	}
	if b.Provider == "manual" || b.Provider == "self-hosted" {
		r := Report{Provider: b.Provider, Currency: host.Currency, ObservedAt: now, Source: "configured monthly budget", Notes: []string{"User-supplied budget; electricity, equipment and other expenses are included only if you budget them."}}
		if b.MonthlyAmount != nil {
			total := *b.MonthlyAmount + host.MonthlyBackupCost
			r.MonthlyEstimate = &total
		}
		return r, nil
	}
	if !force {
		data, err := store.State(ctx, host.ID, "", "billing")
		if err != nil {
			return Report{}, err
		}
		var cached Report
		if json.Unmarshal(data, &cached) == nil && cached.Provider == b.Provider && cached.ResourceID == b.ResourceID && cached.MonthlyEstimate != nil && validMoney(*cached.MonthlyEstimate) && regexp.MustCompile(`^[A-Z]{3}$`).MatchString(cached.Currency) && time.Since(cached.ObservedAt) >= 0 && time.Since(cached.ObservedAt) < 6*time.Hour {
			return cached, nil
		}
	}
	r, err := NewClient().Observe(ctx, *b, now)
	if err != nil {
		return Report{}, err
	}
	if !regexp.MustCompile(`^[A-Z]{3}$`).MatchString(r.Currency) || r.MonthlyEstimate == nil || !validMoney(*r.MonthlyEstimate) {
		return Report{}, fmt.Errorf("provider returned invalid billing amounts or currency")
	}
	if err = store.Transition(ctx, host.ID, "", "billing", r, "", now, nil); err != nil {
		return Report{}, err
	}
	return r, nil
}

func (c Client) Observe(ctx context.Context, b config.Billing, now time.Time) (Report, error) {
	switch b.Provider {
	case "hetzner":
		return c.hetzner(ctx, b, now)
	case "hostinger":
		return c.hostinger(ctx, b, now)
	default:
		return Report{}, fmt.Errorf("provider has no API reader")
	}
}
