package diagnostics

import (
	"github.com/Shumair99/host-ops/internal/model"
)

func Diagnose(r model.Report) []model.Diagnosis {
	states := map[string]model.State{}
	evidence := map[string]string{}
	for _, c := range r.Checks {
		states[c.Layer] = c.State
		evidence[c.Layer] = c.Summary
	}
	findings := []model.Diagnosis{}
	add := func(finding, confidence, layer, investigation string, layers ...string) {
		ev := []string{}
		for _, key := range layers {
			ev = append(ev, key+": "+evidence[key])
		}
		findings = append(findings, model.Diagnosis{HostID: r.HostID, SiteID: r.SiteID, Finding: finding, Confidence: confidence, Layer: layer, Evidence: ev, Investigation: investigation})
	}
	if states["runtime"] == model.Fail {
		add("Application runtime is stopped or inactive", "high", "runtime", "Inspect the application's exit reason and existing deployment logs.", "runtime", "public_http")
	}
	if states["container_health"] == model.Fail && states["runtime"] == model.Pass {
		add("Container runs but its application health check fails", "high", "application", "Inspect application logs and the configured health endpoint.", "runtime", "container_health")
	}
	if states["proxy"] == model.Fail || states["caddy"] == model.Fail {
		layer := "proxy"
		if states["caddy"] == model.Fail {
			layer = "caddy"
		}
		add("Shared reverse proxy is not running", "high", "proxy", "Inspect proxy runtime and logs; identify all sites sharing this proxy.", layer, "local_http", "public_http")
	}
	if states["proxy_route"] == model.Fail {
		add("Loaded proxy configuration differs from the site manifest", "high", "proxy", "Compare the loaded domain/upstream with the protected Caddy configuration before changing it.", "proxy_route", "local_http")
	}
	if states["dns"] == model.Fail {
		add("DNS does not resolve as configured", "high", "dns", "Compare authoritative DNS and recursive resolver answers with the intended origin addresses.", "dns", "local_http", "public_http")
	}
	if states["tls"] == model.Fail {
		add("TLS reachability or certificate validation failed", "medium", "tls", "Check origin reachability, certificate validity and DNS before changing application code.", "tls", "dns")
	}
	if states["public_http"] == model.Fail && states["local_http"] == model.Pass && len(findings) == 0 {
		add("Public endpoint fails while the local application responds", "medium", "public_network", "Compare origin HTTPS with outside-in requests and inspect the reverse proxy.", "local_http", "caddy", "public_http")
	}
	for _, c := range r.Checks {
		if c.State == model.Unknown {
			add("Observation unavailable: "+c.Layer, "unconfirmed", c.Layer, "Verify access, configuration and probe availability; no root cause is confirmed.", c.Layer)
		}
	}
	return findings
}
