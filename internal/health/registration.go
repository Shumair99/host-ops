package health

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/singleflight"
)

const rdapBootstrap = "https://data.iana.org/rdap/dns.json"

type registrationEntry struct {
	check model.Check
	until time.Time
}
type registrationClient struct {
	client         *http.Client
	bootstrapURL   string
	mu             sync.Mutex
	services       map[string]string
	bootstrapUntil time.Time
	cache          map[string]registrationEntry
	flights        singleflight.Group
	slots          chan struct{}
}

func newRegistrationClient() *registrationClient {
	transport := &http.Transport{DialContext: publicRegistryDial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 2 * time.Second, MaxIdleConns: 4, IdleConnTimeout: time.Minute}
	return &registrationClient{
		client:       &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RDAP redirect not followed") }},
		bootstrapURL: rdapBootstrap, cache: map[string]registrationEntry{}, slots: make(chan struct{}, 2),
	}
}

// Resolve once, reject non-public destinations, and connect to the checked IP.
// TLS still verifies the registry URL's hostname. No environment proxy, cookies,
// authentication, insecure TLS or response-driven redirects are used.
func publicRegistryDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("registry address unavailable")
	}
	for _, ip := range addresses {
		if !publicRegistryIP(ip) {
			return nil, errors.New("non-public registry destination rejected")
		}
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	for _, ip := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("registry connection unavailable")
}

func publicRegistryIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

var registrations = newRegistrationClient()

// User-authorized public registry enrichment is optional corroborating
// evidence. An unavailable lookup never changes endpoint availability.
// Only lifecycle/status fields are retained; account ownership is unverified.
func EnrichRegistration(ctx context.Context, s config.Site, checks []model.Check) []model.Check {
	if os.Getenv("HOSTOPS_NO_RDAP") != "" {
		return checks
	}
	failing := false
	for _, c := range checks {
		if c.State == model.Fail && (c.Layer == "dns" || c.Layer == "tcp" || c.Layer == "tls" || c.Layer == "public_http") {
			failing = true
		}
	}
	if !failing {
		return checks
	}
	u, err := url.Parse(s.Health.PublicURL)
	if err != nil {
		return checks
	}
	domain, ok := registrationDomain(u.Hostname())
	if !ok {
		return checks
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c := registrations.lookup(ctx, domain)
	c.HostID, c.SiteID = s.HostID, s.ID
	return append(checks, c)
}

func registrationDomain(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil || len(host) > 253 {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", false
			}
		}
	}
	if _, icann := publicsuffix.PublicSuffix(host); !icann {
		return "", false
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	return domain, err == nil
}

func (r *registrationClient) lookup(ctx context.Context, domain string) model.Check {
	r.mu.Lock()
	entry, ok := r.cache[domain]
	r.mu.Unlock()
	if ok && time.Now().Before(entry.until) {
		return entry.check
	}
	result := r.flights.DoChan("domain:"+domain, func() (any, error) {
		r.mu.Lock()
		cached, exists := r.cache[domain]
		r.mu.Unlock()
		if exists && time.Now().Before(cached.until) {
			return cached.check, nil
		}
		c := r.query(ctx, domain)
		ttl := time.Hour
		if c.Code == "registration_unavailable" {
			ttl = 5 * time.Minute
		} else if c.Code == "registration_not_found" {
			ttl = 15 * time.Minute
		}
		r.mu.Lock()
		if len(r.cache) >= 1024 {
			oldestKey := ""
			var oldest time.Time
			for key, cached := range r.cache {
				if oldestKey == "" || cached.until.Before(oldest) {
					oldestKey, oldest = key, cached.until
				}
			}
			delete(r.cache, oldestKey)
		}
		r.cache[domain] = registrationEntry{c, time.Now().Add(ttl)}
		r.mu.Unlock()
		return c, nil
	})
	select {
	case result := <-result:
		return result.Val.(model.Check)
	case <-ctx.Done():
		return registrationUnavailable("Registry lookup deadline elapsed")
	}
}

func registrationUnavailable(summary string) model.Check {
	return model.Check{Layer: "registration", Code: "registration_unavailable", State: model.Skip, Summary: summary + "; registration cause is unconfirmed", ObservedAt: time.Now().UTC()}
}

func (r *registrationClient) query(ctx context.Context, domain string) model.Check {
	services := r.bootstrap(ctx)
	tld := domain[strings.LastIndexByte(domain, '.')+1:]
	base := services[tld]
	if base == "" {
		return registrationUnavailable("No available IANA RDAP service for this domain")
	}
	endpoint := strings.TrimRight(base, "/") + "/domain/" + domain
	data, status, err := r.get(ctx, endpoint, 1<<20)
	if err != nil {
		return registrationUnavailable("Registry lookup unavailable")
	}
	now := time.Now().UTC()
	if status == http.StatusNotFound {
		var response struct {
			ErrorCode int `json:"errorCode"`
		}
		if json.Unmarshal(data, &response) == nil && response.ErrorCode == 404 {
			return model.Check{Layer: "registration", Code: "registration_not_found", State: model.Warn, Summary: "Registry returned no domain record", ObservedAt: now, Details: []string{"Domain: " + domain, "RDAP source: " + endpoint, "Not proof of availability, expiration or ownership"}}
		}
	}
	if status != http.StatusOK {
		return registrationUnavailable(fmt.Sprintf("Registry returned HTTP %d", status))
	}
	return registrationResponse(data, domain, endpoint, now)
}

func (r *registrationClient) bootstrap(ctx context.Context) map[string]string {
	r.mu.Lock()
	services, until := r.services, r.bootstrapUntil
	r.mu.Unlock()
	if time.Now().Before(until) {
		return services
	}
	result := r.flights.DoChan("bootstrap", func() (any, error) {
		r.mu.Lock()
		cached, until := r.services, r.bootstrapUntil
		r.mu.Unlock()
		if time.Now().Before(until) {
			return cached, nil
		}
		data, status, err := r.get(ctx, r.bootstrapURL, 4<<20)
		var response struct {
			Services [][][]string `json:"services"`
		}
		services := map[string]string{}
		ttl := 5 * time.Minute
		if err == nil && status == http.StatusOK && json.Unmarshal(data, &response) == nil {
			for _, service := range response.Services {
				if len(service) != 2 {
					continue
				}
				for _, base := range service[1] {
					if !registryURL(base) {
						continue
					}
					for _, tld := range service[0] {
						services[strings.ToLower(tld)] = base
					}
					break
				}
			}
			if len(services) > 0 {
				ttl = 24 * time.Hour
			}
		}
		r.mu.Lock()
		r.services, r.bootstrapUntil = services, time.Now().Add(ttl)
		r.mu.Unlock()
		return services, nil
	})
	select {
	case result := <-result:
		return result.Val.(map[string]string)
	case <-ctx.Done():
		return nil
	}
}

func registryURL(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" && u.Port() != "443" {
		return false
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		return publicRegistryIP(ip)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host != "localhost" && !strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local")
}

func (r *registrationClient) get(ctx context.Context, endpoint string, limit int64) ([]byte, int, error) {
	if !registryURL(endpoint) {
		return nil, 0, errors.New("invalid registry URL")
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", "HostOps/registration-evidence")
	response, err := r.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType != "application/rdap+json" && contentType != "application/json" {
		return nil, response.StatusCode, errors.New("invalid registry media type")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, response.StatusCode, errors.New("RDAP response exceeds limit")
	}
	return data, response.StatusCode, err
}

func registrationResponse(data []byte, domain, source string, now time.Time) model.Check {
	var response struct {
		Class  string   `json:"objectClassName"`
		Name   string   `json:"ldhName"`
		Status []string `json:"status"`
		Events []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
	}
	if json.Unmarshal(data, &response) != nil || response.Class != "domain" || !strings.EqualFold(strings.TrimSuffix(response.Name, "."), domain) {
		return registrationUnavailable("Registry response did not identify the requested domain")
	}
	c := model.Check{Layer: "registration", Code: "registration_record", State: model.Pass, Summary: "Registry domain record found; ownership is unverified", ObservedAt: now, Details: []string{"Domain: " + domain, "RDAP source: " + source}}
	statuses := map[string]bool{}
	for _, status := range response.Status {
		switch status {
		case "pending delete", "redemption period", "client hold", "server hold", "auto renew period":
			statuses[status] = true
		}
	}
	lifecycle := []string{}
	for _, status := range []string{"pending delete", "redemption period", "client hold", "server hold", "auto renew period"} {
		if statuses[status] {
			lifecycle = append(lifecycle, status)
		}
	}
	if len(lifecycle) > 0 {
		c.Details = append(c.Details, "Registry lifecycle status: "+strings.Join(lifecycle, ", "))
	}
	var expiration time.Time
	conflicting := false
	for _, event := range response.Events {
		if event.Action == "expiration" {
			if at, err := time.Parse(time.RFC3339, event.Date); err == nil {
				if !expiration.IsZero() && !expiration.Equal(at) {
					conflicting = true
				}
				expiration = at
			}
		}
	}
	if conflicting {
		c.Details = append(c.Details, "Conflicting registry expiration dates; elapsed expiration is unconfirmed")
	} else if !expiration.IsZero() {
		c.Details = append(c.Details, "Registry expiration: "+expiration.UTC().Format(time.RFC3339))
	}
	switch {
	case statuses["pending delete"]:
		c.Code, c.Summary = "registration_pending_delete", "Registry reports pending deletion"
	case statuses["redemption period"]:
		c.Code, c.Summary = "registration_redemption", "Registry reports a redemption period"
	case statuses["client hold"] || statuses["server hold"]:
		c.Code, c.Summary = "registration_hold", "Registry reports a domain hold"
	case !conflicting && !expiration.IsZero() && !expiration.After(now):
		c.Code, c.Summary = "registration_expiry_elapsed", "Registry expiration date has elapsed"
	case statuses["auto renew period"]:
		c.Code, c.Summary = "registration_auto_renew", "Registry reports an auto-renewal grace period"
	}
	if c.Code != "registration_record" {
		c.State = model.Warn
	}
	return c
}
