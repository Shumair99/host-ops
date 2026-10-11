package diagnostics

import (
	"fmt"
	"github.com/Shumair99/host-ops/internal/model"
)

func Diagnose(r model.Report) []model.Diagnosis {
	states := map[string]model.State{}
	evidence := map[string]string{}
	rank := map[model.State]int{model.Skip: 0, model.Pass: 1, model.Warn: 2, model.Unknown: 3, model.Fail: 4}
	for _, c := range r.Checks {
		if rank[c.State] >= rank[states[c.Layer]] {
			states[c.Layer] = c.State
			evidence[c.Layer] = c.Summary
		}
	}
	findings := []model.Diagnosis{}
	add := func(finding, confidence, layer, investigation string, layers ...string) {
		ev := []string{}
		for _, key := range layers {
			if summary, ok := evidence[key]; ok {
				ev = append(ev, fmt.Sprintf("%s [%s]: %s", key, states[key], summary))
			}
		}
		findings = append(findings, model.Diagnosis{HostID: r.HostID, SiteID: r.SiteID, Finding: finding, Confidence: confidence, Layer: layer, Evidence: ev, Investigation: investigation})
	}
	if states["runtime"] == model.Fail {
		add("Application runtime is stopped or inactive", "high", "runtime", "Inspect the recorded exit state and deployment logs. A stopped runtime does not establish why it stopped.", "runtime", "public_http")
	}
	if states["container_health"] == model.Fail && states["runtime"] == model.Pass {
		add("Container runs but its application health check fails", "high", "application", "Inspect application logs and the configured health endpoint.", "runtime", "container_health")
	}
	if states["proxy"] == model.Fail || states["caddy"] == model.Fail {
		layer := "proxy"
		if states["caddy"] == model.Fail {
			layer = "caddy"
		}
		add("Observed reverse proxy runtime is stopped or inactive", "high", "proxy", "Inspect proxy runtime and logs; review the other sites using that observed proxy.", layer, "local_http", "public_http")
	}
	if states["proxy_route"] == model.Fail {
		finding := "Loaded proxy configuration differs from the site manifest"
		for _, c := range r.Checks {
			if c.Layer == "proxy_route" && c.State == model.Fail {
				if c.Code == "proxy_domain_missing" {
					finding = "Domain is missing from the loaded Caddy configuration"
				}
				if c.Code == "proxy_upstream_mismatch" {
					finding = "Loaded Caddy upstream differs from the expected backend"
				}
			}
		}
		add(finding, "high", "proxy", "Compare the active domain/upstream with the intended configuration before editing shared Caddy routes.", "proxy_route", "local_http")
	}
	specific := probeFindings(r, states)
	findings = append(findings, specific...)
	if states["dns"] == model.Fail && !findingLayer(specific, "dns") {
		add("DNS lookup failed; its underlying cause is unconfirmed", "high", "dns", "Compare authoritative DNS with this machine's resolver. DNS failure alone does not establish domain expiration or ownership.", "dns", "local_http", "public_http")
	}
	if states["tls"] == model.Fail && states["dns"] != model.Fail && states["tcp"] != model.Fail && !findingLayer(specific, "tls") {
		add("TLS reachability or certificate validation failed", "medium", "tls", "Check origin reachability, certificate validity and DNS before changing application code.", "tls", "dns")
	}
	if states["public_http"] == model.Fail && states["local_http"] == model.Pass && len(findings) == 0 {
		add("Public endpoint fails while the local application responds", "medium", "public_network", "Compare origin HTTPS with outside-in requests and inspect the reverse proxy.", "local_http", "caddy", "public_http")
	}
	seen := map[string]bool{}
	for _, c := range r.Checks {
		if c.State == model.Unknown && !seen[c.Layer] {
			seen[c.Layer] = true
			add("Observation unavailable: "+c.Layer, "unconfirmed", c.Layer, "Verify access, configuration and probe availability; no root cause is confirmed.", c.Layer)
		}
	}
	return findings
}
