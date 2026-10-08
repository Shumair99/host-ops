package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

func HTTP(ctx context.Context, s config.Site, rawURL, layer string) model.Check {
	check := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: layer, State: model.Unknown, Summary: "Endpoint not configured"}
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
			return fmt.Errorf("redirect limit")
		}
		if req.URL.User != nil {
			return fmt.Errorf("redirect credentials forbidden")
		}
		if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("HTTPS downgrade forbidden")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		check.Summary = "Invalid endpoint"
		return check
	}
	req.Header.Set("User-Agent", "Amadeus-HostOps/0.1")
	resp, err := client.Do(req)
	check.LatencyMS = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		check.State = model.Fail
		check.Summary = "HTTP request failed (DNS, connection, TLS, redirect or timeout)"
		return check
	}
	resp.Body.Close()
	check.StatusCode = resp.StatusCode
	check.State = model.Pass
	check.Summary = fmt.Sprintf("HTTP %d; %.0f ms", resp.StatusCode, check.LatencyMS)
	if s.Health.Automatic {
		if resp.StatusCode >= 500 {
			check.State = model.Fail
		}
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			check.State = model.Warn
			check.Summary += "; endpoint responds, application health is unconfirmed"
		}
	} else if resp.StatusCode != s.Health.ExpectedStatus {
		check.State = model.Fail
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
	check.StatusCode, _ = strconv.Atoi(fields[0])
	seconds, _ := strconv.ParseFloat(fields[1], 64)
	check.LatencyMS = seconds * 1000
	check.State = model.Pass
	if check.StatusCode != s.Health.ExpectedStatus {
		check.State = model.Fail
	}
	check.Summary = fmt.Sprintf("HTTP %d; %.0f ms", check.StatusCode, check.LatencyMS)
	return check
}

func Network(ctx context.Context, s config.Site) []model.Check {
	checks := []model.Check{}
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
	dns := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "dns", State: model.Pass, Summary: strings.Join(addresses, ", ")}
	if err != nil || len(addresses) == 0 {
		dns.State = model.Fail
		dns.Summary = "DNS resolution failed"
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
				dns.Summary = "DNS resolves to unexpected address: " + actual
				break
			}
		}
	}
	checks = append(checks, dns)
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
		tcp.Summary = "Public TCP connection failed"
	} else {
		connection.Close()
	}
	checks = append(checks, tcp)
	tlsCheck := model.Check{HostID: s.HostID, SiteID: s.ID, Layer: "tls", State: model.Skip, Summary: "Public endpoint uses HTTP"}
	if u.Scheme == "https" {
		port := u.Port()
		if port == "" {
			port = "443"
		}
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: s.Timeout()}, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
		if err != nil {
			tlsCheck.State = model.Fail
			tlsCheck.Summary = "TLS connection or certificate validation failed"
		} else {
			defer conn.Close()
			state := conn.(*tls.Conn).ConnectionState()
			cert := state.PeerCertificates[0]
			days := int(time.Until(cert.NotAfter).Hours() / 24)
			tlsCheck.State = model.Pass
			tlsCheck.Summary = fmt.Sprintf("Certificate verified; expires in %d days", days)
			if days < 14 {
				tlsCheck.State = model.Warn
			}
		}
	}
	return append(checks, tlsCheck)
}

func External(ctx context.Context, s config.Site) model.Report {
	checks := Network(ctx, s)
	checks = append(checks, HTTP(ctx, s, s.Health.PublicURL, "public_http"))
	return model.Report{HostID: s.HostID, SiteID: s.ID, Name: s.Name, ObservedAt: time.Now().UTC(), Vantage: "external watcher (deploy separately from production)", Runtime: model.Runtime{HostID: s.HostID, SiteID: s.ID, Type: "external", Status: "not_applicable"}, Overall: model.Overall(checks), Checks: checks, Diagnoses: []model.Diagnosis{}}
}
