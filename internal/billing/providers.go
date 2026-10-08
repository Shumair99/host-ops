package billing

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
)

type hetznerServer struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Created      time.Time `json:"created"`
	BackupWindow *string   `json:"backup_window"`
	ServerType   struct {
		Name string `json:"name"`
	} `json:"server_type"`
	Datacenter struct {
		Location struct {
			Name string `json:"name"`
		} `json:"location"`
	} `json:"datacenter"`
}

type price struct {
	Gross string `json:"gross"`
}

func (c Client) hetzner(ctx context.Context, b config.Billing, now time.Time) (Report, error) {
	if _, err := strconv.ParseUint(b.ResourceID, 10, 64); err != nil {
		return Report{}, fmt.Errorf("select a numeric Hetzner server ID")
	}
	var resource struct {
		Server hetznerServer `json:"server"`
	}
	if err := c.get(ctx, "hetzner", "/servers/"+b.ResourceID, b.TokenEnv, &resource); err != nil {
		return Report{}, err
	}
	var pricing struct {
		Pricing struct {
			Currency string `json:"currency"`
			Backup   struct {
				Percentage string `json:"percentage"`
			} `json:"server_backup"`
			ServerTypes []struct {
				Name   string `json:"name"`
				Prices []struct {
					Location string `json:"location"`
					Hourly   price  `json:"price_hourly"`
					Monthly  price  `json:"price_monthly"`
				} `json:"prices"`
			} `json:"server_types"`
		} `json:"pricing"`
	}
	if err := c.get(ctx, "hetzner", "/pricing", b.TokenEnv, &pricing); err != nil {
		return Report{}, err
	}
	s := resource.Server
	if strconv.FormatInt(s.ID, 10) != b.ResourceID || s.Created.IsZero() || s.Created.After(now) {
		return Report{}, fmt.Errorf("provider server identity or creation time is invalid")
	}
	for _, kind := range pricing.Pricing.ServerTypes {
		if kind.Name != s.ServerType.Name {
			continue
		}
		for _, location := range kind.Prices {
			if location.Location != s.Datacenter.Location.Name {
				continue
			}
			hourly, err := strconv.ParseFloat(location.Hourly.Gross, 64)
			if err != nil || !validMoney(hourly) {
				return Report{}, fmt.Errorf("invalid provider hourly price")
			}
			monthly, err := strconv.ParseFloat(location.Monthly.Gross, 64)
			if err != nil || !validMoney(monthly) {
				return Report{}, fmt.Errorf("invalid provider monthly cap")
			}
			estimate := monthly
			if s.BackupWindow != nil {
				percent, err := strconv.ParseFloat(pricing.Pricing.Backup.Percentage, 64)
				if err != nil || !validMoney(percent) {
					return Report{}, fmt.Errorf("backup pricing unavailable")
				}
				estimate += monthly * percent / 100
			}
			start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			if s.Created.After(start) {
				start = s.Created
			}
			accrued := math.Min(monthly, math.Ceil(now.Sub(start).Hours())*hourly)
			return Report{Provider: "hetzner", ResourceID: b.ResourceID, Plan: s.ServerType.Name + " / " + location.Location, Currency: pricing.Pricing.Currency, ObservedAt: now, Source: "provider API pricing; estimate", MonthlyEstimate: &estimate, ServerMonthlyCap: &monthly, HourlyRate: &hourly, ServerMonthToDate: &accrued, Notes: []string{"Monthly run rate includes the server and enabled server backups, using account gross pricing.", "IP addresses, volumes, snapshots, excess traffic, credits and invoice adjustments are excluded.", "Month-to-date is the server-only estimate assuming its current plan throughout this month; resize history is not inferred.", "Hetzner bills allocated lifetime, including powered-off hours; CPU utilisation does not reduce its bill."}}, nil
		}
	}
	return Report{}, fmt.Errorf("no provider price for this server type and location")
}

type subscription struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Period   int    `json:"billing_period"`
	Unit     string `json:"billing_period_unit"`
	Currency string `json:"currency_code"`
	Total    *int64 `json:"total_price"`
	Renewal  *int64 `json:"renewal_price"`
}

func (c Client) hostinger(ctx context.Context, b config.Billing, now time.Time) (Report, error) {
	var subscriptions []subscription
	if err := c.get(ctx, "hostinger", "/billing/v1/subscriptions", b.TokenEnv, &subscriptions); err != nil {
		return Report{}, err
	}
	for _, s := range subscriptions {
		if s.ID != b.ResourceID {
			continue
		}
		if s.Status != "active" || s.Period <= 0 || s.Total == nil || *s.Total < 0 || s.Renewal != nil && *s.Renewal < 0 {
			return Report{}, fmt.Errorf("subscription has no active valid billing period")
		}
		months := float64(s.Period)
		switch s.Unit {
		case "month":
		case "year":
			months *= 12
		case "day":
			months *= 12 / 365.2425
		case "week":
			months *= 7 * 12 / 365.2425
		default:
			return Report{}, fmt.Errorf("unsupported subscription billing period")
		}
		monthly := float64(*s.Total) / 100 / months
		renewal := "Renewal amount unavailable from the provider."
		if s.Renewal != nil {
			renewal = fmt.Sprintf("Reported renewal: %.2f %s per billing period; introductory and renewal prices may differ.", float64(*s.Renewal)/100, s.Currency)
		}
		return Report{Provider: "hostinger", ResourceID: b.ResourceID, Plan: s.Name, Currency: s.Currency, ObservedAt: now, MonthlyEstimate: &monthly, Source: "provider subscription; monthly equivalent", Notes: []string{fmt.Sprintf("Current subscription: %.2f %s per %d %s(s); normalized to a monthly equivalent.", float64(*s.Total)/100, s.Currency, s.Period, s.Unit), renewal, "This is subscription evidence, not an invoice or a CPU-usage discount. Add-on subscriptions are excluded.", "Day/week periods use the average calendar month for normalization."}}, nil
	}
	return Report{}, fmt.Errorf("selected subscription was not returned by this account")
}

func (c Client) Resources(ctx context.Context, provider, env string) ([]Resource, error) {
	resources := []Resource{}
	switch provider {
	case "hetzner":
		page := 1
		for scanned := 0; scanned < 20; scanned++ {
			var response struct {
				Servers []hetznerServer `json:"servers"`
				Meta    struct {
					Pagination struct {
						Next *int `json:"next_page"`
					} `json:"pagination"`
				} `json:"meta"`
			}
			if err := c.get(ctx, provider, fmt.Sprintf("/servers?page=%d&per_page=50", page), env, &response); err != nil {
				return nil, err
			}
			for _, s := range response.Servers {
				resources = append(resources, Resource{ID: strconv.FormatInt(s.ID, 10), Name: s.Name, Plan: s.ServerType.Name, Location: s.Datacenter.Location.Name})
			}
			if response.Meta.Pagination.Next == nil {
				return resources, nil
			}
			if *response.Meta.Pagination.Next <= page {
				return nil, fmt.Errorf("invalid provider pagination")
			}
			page = *response.Meta.Pagination.Next
		}
		return nil, fmt.Errorf("provider listing exceeds 1,000 servers; select a known resource ID")
	case "hostinger":
		var subscriptions []subscription
		if err := c.get(ctx, provider, "/billing/v1/subscriptions", env, &subscriptions); err != nil {
			return nil, err
		}
		for _, s := range subscriptions {
			if s.Status == "active" {
				resources = append(resources, Resource{ID: s.ID, Name: s.Name, Plan: s.Name})
			}
		}
		return resources, nil
	default:
		return nil, fmt.Errorf("billing list supports hetzner or hostinger")
	}
}
