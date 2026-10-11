package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/diagnostics"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

func HTTP(ctx context.Context, s config.Site, rawURL, layer string) model.Check {
	check := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: layer, State: model.Unknown, Summary: "Endpoint not configured", ObservedAt: time.Now().UTC()}
	if rawURL == "" {
		return check
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout())
	defer cancel()
	start := time.Now()
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: (&net.Dialer{Timeout: s.Timeout()}).DialContext, TLSHandshakeTimeout: s.Timeout()}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: s.Timeout(), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errRedirectLimit
		}
		if req.URL.User != nil {
			return errRedirectCredentials
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errHTTPSDowngrade
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		check.Code = "endpoint_invalid"
		check.Summary = "Invalid endpoint"
		return check
	}
	req.Header.Set("User-Agent", "Amadeus-HostOps/0.1")
	resp, err := client.Do(req)
	check.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		check.State = model.Fail
		check.Code, check.Summary = probeFailure(err)
		return check
	}
	resp.Body.Close()
	check.StatusCode = resp.StatusCode
	check.State = model.Pass
	check.Code = "http_ok"
	check.Summary = fmt.Sprintf("HTTP %d; %.0f ms", resp.StatusCode, check.LatencyMS)
	if s.Health.Automatic {
		if resp.StatusCode >= 500 {
			check.State = model.Fail
			check.Code = "http_server_error"
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			check.State = model.Warn
			check.Code = "http_client_error"
			check.Summary += "; endpoint responds, application health is unconfirmed"
		}
	} else if resp.StatusCode != s.Health.ExpectedStatus {
		check.State = model.Fail
		check.Code = "http_unexpected_status"
		check.Details = []string{fmt.Sprintf("Expected HTTP %d", s.Health.ExpectedStatus)}
	}
	return check
}

func LocalHTTP(ctx context.Context, runner platform.Runner, s config.Site, r model.Runtime) model.Check {
	check := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "local_http", State: model.Unknown, Summary: "Local endpoint not configured"}
	endpoint := s.Health.LocalURL
	if endpoint == "" && s.Runtime.Type == "docker" && s.Runtime.Port > 0 {
		ip := r.IPs[s.Runtime.Network]
		if ip != "" {
			endpoint = "http://" + net.JoinHostPort(ip, strconv.Itoa(s.Runtime.Port)) + s.Health.Path
		}
	}
	if endpoint == "" {
		return check
	}
	data, err := runner.Run(ctx, "curl", "--silent", "--show-error", "--output", "/dev/null", "--max-time", strconv.Itoa(s.Health.TimeoutSeconds), "--write-out", "%{http_code} %{time_total}", "--", endpoint)
	if err != nil {
		check.Summary = "Local HTTP probe failed or host inspection unavailable"
		return check
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		check.Summary = "Local HTTP result invalid"
		return check
	}
	status, statusErr := strconv.Atoi(fields[0])
	seconds, durationErr := strconv.ParseFloat(fields[1], 64)
	if statusErr != nil || durationErr != nil || status < 100 || status > 599 || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		check.Summary = "Local HTTP result invalid"
		return check
	}
	check.StatusCode = status
	check.LatencyMS = seconds * 1000
	check.State = model.Pass
	check.Code = "http_ok"
	if check.StatusCode != s.Health.ExpectedStatus {
		check.State = model.Fail
		check.Code = "http_unexpected_status"
		check.Details = []string{fmt.Sprintf("Expected HTTP %d", s.Health.ExpectedStatus)}
	}
	check.Summary = fmt.Sprintf("HTTP %d; %.0f ms", check.StatusCode, check.LatencyMS)
	return check
}

func Network(ctx context.Context, s config.Site) (checks []model.Check) {
	checks = []model.Check{}
	at := time.Now().UTC()
	defer func() {
		for i := range checks {
			checks[i].ObservedAt = at
		}
	}()
	u, err := url.Parse(s.Health.PublicURL)
	if err != nil || u.Hostname() == "" {
		for _, layer := range []string{"dns", "tcp", "tls"} {
			checks = append(checks, model.Check{HostID: s.HostID, SiteID: s.ID, Layer: layer, State: model.Unknown, Summary: "Public endpoint not configured"})
		}
		return checks
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout())
	defer cancel()
	addresses, err := net.DefaultResolver.LookupHost(ctx, u.Hostname())
	sort.Strings(addresses)
	dns := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "dns", Code: "dns_resolved", State: model.Pass, Summary: strings.Join(addresses, ", ")}
	if err != nil || len(addresses) == 0 {
		dns.State = model.Fail
		dns.Code, dns.Summary = probeFailure(err)
		if err == nil {
			dns.Code, dns.Summary = "dns_not_found", "Resolver found no usable DNS address for this hostname"
		}
	} else if len(s.Proxy.ExpectedIPs) > 0 {
		for _, actual := range addresses {
			found := false
			for _, expected := range s.Proxy.ExpectedIPs {
				if actual == expected {
					found = true
				}
			}
			if !found {
				dns.State = model.Fail
				dns.Code = "dns_unexpected_address"
				dns.Summary = "DNS resolves to unexpected address: " + actual
				dns.Details = []string{"Expected addresses: " + strings.Join(s.Proxy.ExpectedIPs, ", "), "Observed addresses: " + strings.Join(addresses, ", ")}
				break
			}
		}
	}
	checks = append(checks, dns)
	if err != nil || len(addresses) == 0 {
		for _, layer := range []string{"tcp", "tls"} {
			checks = append(checks, model.Check{HostID: s.HostID, SiteID: s.ID, Layer: layer, Code: "blocked_by_dns", State: model.Skip, Summary: "Not probed: DNS address lookup failed"})
		}
		return checks
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	tcp := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "tcp", State: model.Pass, Summary: "Public TCP endpoint reachable"}
	connection, tcpErr := (&net.Dialer{Timeout: s.Timeout()}).DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if tcpErr != nil {
		tcp.State = model.Fail
		tcp.Code, tcp.Summary = probeFailure(tcpErr)
	} else {
		connection.Close()
	}
	checks = append(checks, tcp)
	tlsCheck := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "tls", State: model.Skip, Summary: "Public endpoint uses HTTP"}
	if tcpErr != nil && u.Scheme == "https" {
		tlsCheck.Code, tlsCheck.Summary = "blocked_by_tcp", "Not probed: TCP endpoint was unreachable"
		return append(checks, tlsCheck)
	}
	if u.Scheme == "https" {
		port := u.Port()
		if port == "" {
			port = "443"
		}
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: s.Timeout()}, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
		if err != nil {
			tlsCheck.State = model.Fail
			tlsCheck.Code, tlsCheck.Summary = probeFailure(err)
			if tlsCheck.Code == "request_failed" {
				tlsCheck.Code, tlsCheck.Summary = "tls_handshake_failed", "TLS handshake failed; certificate cause is unconfirmed"
			}
		} else {
			defer conn.Close()
			state := conn.(*tls.Conn).ConnectionState()
			cert := state.PeerCertificates[0]
			days := int(time.Until(cert.NotAfter).Hours() / 24)
			tlsCheck.State = model.Pass
			tlsCheck.Code = "tls_verified"
			tlsCheck.Summary = fmt.Sprintf("Certificate verified; expires in %d days", days)
			if days < 14 {
				tlsCheck.State = model.Warn
				tlsCheck.Code = "tls_expiring_soon"
			}
		}
	}
	return append(checks, tlsCheck)
}

func External(ctx context.Context, s config.Site) model.Report {
	checks := Network(ctx, s)
	checks = append(checks, HTTP(ctx, s, s.Health.PublicURL, "public_http"))
	checks = EnrichRegistration(ctx, s, checks)
	r := model.Report{HostID: s.HostID, SiteID: s.ID, Name: s.Name, ObservedAt: time.Now().UTC(), Vantage: "external watcher (deploy separately from production)", Runtime: model.Runtime{HostID: s.HostID, SiteID: s.ID, Type: "external", Status: "not_applicable"}, Overall: model.Overall(checks), Checks: checks}
	r.Diagnoses = diagnostics.Diagnose(r)
	return r
}
