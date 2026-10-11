package diagnostics

import (
	"fmt"
	"strings"

	"github.com/Shumair99/host-ops/internal/model"
)

func findingLayer(findings []model.Diagnosis, layer string) bool {
	for _, d := range findings {
		if d.Layer == layer {
			return true
		}
	}
	return false
}

func probeFindings(r model.Report, states map[string]model.State) []model.Diagnosis {
	findings := []model.Diagnosis{}
	add := func(finding, confidence, layer, next string, layers ...string) {
		if findingLayer(findings, layer) {
			return
		}
		evidence, seen := []string{}, map[string]bool{}
		for _, key := range layers {
			for _, c := range r.Checks {
				if c.Layer != key {
					continue
				}
				lines := append([]string{fmt.Sprintf("%s [%s]: %s", key, c.State, c.Summary)}, c.Details...)
				if !c.ObservedAt.IsZero() {
					lines = append(lines, "Evidence observed: "+c.ObservedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
				}
				for _, line := range lines {
					if !seen[line] {
						evidence, seen[line] = append(evidence, line), true
					}
				}
			}
		}
		findings = append(findings, model.Diagnosis{HostID: r.HostID, SiteID: r.SiteID, Finding: finding, Confidence: confidence, Layer: layer, Investigation: next, Evidence: evidence})
	}
	for _, c := range r.Checks {
		if c.Layer != "registration" || c.State != model.Warn {
			continue
		}
		finding, next := "", ""
		switch c.Code {
		case "registration_hold":
			finding, next = "Registry reports a domain hold", "A hold prevents DNS publication. Its reason and account ownership are unverified; check the registrar only if this domain is still intended for use."
		case "registration_expiry_elapsed":
			finding, next = "Registry expiration date has elapsed", "The date alone does not prove loss of ownership or the reason for DNS failure. Registrar renewal and grace-period records may differ."
		case "registration_redemption":
			finding, next = "Registry reports a domain redemption period", "This is a registration lifecycle state. Ownership and renewal intent remain unverified."
		case "registration_pending_delete":
			finding, next = "Registry reports pending domain deletion", "Review the registration lifecycle only if this hostname is still intended for use."
		case "registration_not_found":
			finding, next = "Registry returned no record for this domain", "A registry 404 does not prove previous expiration, availability to purchase, or loss of account ownership."
		case "registration_auto_renew":
			finding, next = "Registry reports an auto-renewal grace period", "Registry auto-renewal is not proof of payment, registrar renewal or continued ownership."
		}
		if finding != "" {
			add(finding, "high", "registration", next, "registration", "dns")
		}
	}
	dnsFailure := states["dns"] == model.Fail
	for _, c := range r.Checks {
		if c.State != model.Fail || !strings.HasPrefix(c.Code, "dns_") {
			continue
		}
		dnsFailure = true
		finding, next := "DNS resolver failed; registration state is unconfirmed", "Compare authoritative DNS with this machine's resolver. DNS failure alone does not establish domain expiration or ownership."
		switch c.Code {
		case "dns_not_found":
			finding = "Hostname has no usable address in this resolver's DNS answer"
		case "dns_timeout":
			finding = "DNS resolver timed out; registration state is unconfirmed"
		case "dns_unexpected_address":
			finding, next = "DNS resolves outside the configured expected addresses", "Compare observed and expected addresses. A CDN, intentional migration or stale configuration may explain the difference."
		}
		add(finding, "high", "dns", next, "dns", "public_http", "registration")
	}
	if !dnsFailure {
		for _, c := range r.Checks {
			if c.State != model.Fail || !strings.HasPrefix(c.Code, "tls_") {
				continue
			}
			finding, confidence := c.Summary, "high"
			if c.Code == "tls_handshake_failed" {
				confidence = "unconfirmed"
			}
			add(finding, confidence, "tls", "Inspect the presented certificate, hostname, trust store and machine clock. This does not establish a Caddy configuration error.", "tls", "public_http", "dns")
		}
		for _, c := range r.Checks {
			if c.State != model.Fail {
				continue
			}
			if c.Code == "connection_refused" || c.Code == "network_unreachable" {
				add(c.Summary, "high", "public_network", "Check listener, routing and firewall from this probe's vantage. The failing component is unconfirmed.", "tcp", "public_http", "dns")
			}
		}
		if states["tcp"] == model.Fail && !findingLayer(findings, "public_network") {
			add("Public TCP endpoint could not be reached", "medium", "public_network", "Check listener, routing and firewall. These probes do not identify which component caused the connection failure.", "tcp", "public_http", "dns")
		}
	}
	if states["local_http"] == model.Fail && states["runtime"] != model.Fail && states["container_health"] != model.Fail {
		add("Local application health endpoint fails", "high", "application", "Inspect the local status and health endpoint. A responding runtime alone does not prove application health.", "local_http", "runtime")
	}
	for _, c := range r.Checks {
		if c.Layer != "public_http" || c.State != model.Fail && c.State != model.Warn {
			continue
		}
		if strings.HasPrefix(c.Code, "redirect_") {
			add(c.Summary, "high", "http", "Inspect redirect targets and HTTPS policy. The probe rejects credentials, downgrade redirects and excessive redirects.", "public_http")
		} else if c.StatusCode > 0 {
			if c.StatusCode >= 500 && (states["runtime"] == model.Fail || states["container_health"] == model.Fail || states["proxy"] == model.Fail || states["caddy"] == model.Fail || states["proxy_route"] == model.Fail) {
				continue
			}
			finding, confidence, layer := fmt.Sprintf("Public endpoint returns HTTP %d", c.StatusCode), "high", "http"
			next := "Inspect the response-producing component and expected health endpoint. A status code alone does not identify the faulty component."
			if (c.StatusCode == 502 || c.StatusCode == 504) && states["local_http"] == model.Pass {
				finding, confidence, layer = fmt.Sprintf("Public gateway returns HTTP %d while the local application responds", c.StatusCode), "medium", "proxy"
				next = "Inspect proxy-to-upstream reachability and timeouts. This pattern does not prove the Caddy configuration is wrong."
			} else if c.StatusCode == 401 || c.StatusCode == 403 {
				next = "This endpoint requires authentication or denies access. Verify whether the health path is intended to be public."
			} else if c.StatusCode == 404 {
				next = "Verify the health path and host route. A 404 does not show that the application process is stopped."
			}
			add(finding, confidence, layer, next, "public_http", "local_http")
		} else if len(findings) == 0 && c.Code != "" && states["local_http"] != model.Pass && states["runtime"] != model.Fail && states["container_health"] != model.Fail && states["proxy"] != model.Fail && states["caddy"] != model.Fail && states["proxy_route"] != model.Fail {
			add("Public request failed; root cause is unconfirmed", "unconfirmed", "http", "Compare DNS, TCP, TLS and application evidence before assigning a cause.", "public_http")
		}
	}
	return findings
}
