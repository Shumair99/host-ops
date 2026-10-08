package caddy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

// Upstreams walks loaded Caddy JSON while preserving enclosing host matches.
// A route for one domain cannot borrow the upstream of a sibling domain.
func Upstreams(data []byte, domain string) ([]string, bool, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, err
	}
	seen, found := map[string]bool{}, false
	var walk func(any, bool, bool)
	walk = func(node any, allowed, matches bool) {
		switch n := node.(type) {
		case []any:
			for _, child := range n {
				walk(child, allowed, matches)
			}
		case map[string]any:
			if matchers, ok := n["match"].([]any); ok {
				hasHost, hostMatches := false, false
				for _, m := range matchers {
					if object, ok := m.(map[string]any); ok {
						if hosts, ok := object["host"].([]any); ok {
							hasHost = true
							for _, host := range hosts {
								if host == domain {
									hostMatches = true
								}
							}
						}
					}
				}
				if hasHost {
					allowed = allowed && hostMatches
					matches = matches || hostMatches
					if allowed && matches {
						found = true
					}
				}
			}
			if allowed && matches && n["handler"] == "reverse_proxy" {
				if list, ok := n["upstreams"].([]any); ok {
					for _, u := range list {
						if object, ok := u.(map[string]any); ok {
							if dial, ok := object["dial"].(string); ok {
								seen[dial] = true
							}
						}
					}
				}
			}
			for key, child := range n {
				if key != "match" && key != "upstreams" {
					walk(child, allowed, matches)
				}
			}
		}
	}
	walk(root, true, false)
	upstreams := []string{}
	for u := range seen {
		upstreams = append(upstreams, u)
	}
	sort.Strings(upstreams)
	return upstreams, found, nil
}

func Inspect(ctx context.Context, runner platform.Runner, h config.Host, s config.Site) []model.Check {
	check := func(layer string, state model.State, summary string) model.Check {
		return model.Check{HostID: h.ID, SiteID: s.ID, Layer: layer, State: state, Summary: summary}
	}
	checks := []model.Check{}
	if h.CaddyContainer != "" {
		data, err := runner.Run(ctx, "docker", "container", "inspect", "--format", "{{.State.Status}}", "--", h.CaddyContainer)
		if err != nil {
			return []model.Check{check("caddy", model.Unknown, "Caddy runtime could not be inspected")}
		}
		if stringTrim(data) != "running" {
			return []model.Check{check("caddy", model.Fail, "Caddy container is not running")}
		}
		checks = append(checks, check("caddy", model.Pass, "Caddy container running"))
	}
	admin := h.CaddyAdminURL
	if admin == "" {
		admin = "http://127.0.0.1:2019"
	}
	var data []byte
	var err error
	if h.CaddyContainer != "" {
		data, err = runner.Run(ctx, "docker", "exec", h.CaddyContainer, "wget", "-qO-", admin+"/config/apps/http/servers/")
	} else {
		data, err = runner.Run(ctx, "curl", "--silent", "--show-error", "--max-time", "5", admin+"/config/apps/http/servers/")
	}
	if err != nil {
		return append(checks, check("proxy_route", model.Unknown, "Loaded Caddy routes unavailable"))
	}
	if h.CaddyContainer == "" {
		checks = append(checks, check("caddy", model.Pass, "Caddy admin API reachable"))
	}
	upstreams, found, err := Upstreams(data, s.Proxy.Domain)
	if err != nil {
		return append(checks, check("proxy_route", model.Unknown, "Loaded Caddy routes invalid"))
	}
	if !found {
		return append(checks, check("proxy_route", model.Fail, "Expected domain missing from loaded Caddy routes"))
	}
	if s.Proxy.Upstream != "" {
		for _, u := range upstreams {
			if u == s.Proxy.Upstream {
				return append(checks, check("proxy_route", model.Pass, "Loaded upstream: "+u))
			}
		}
		return append(checks, check("proxy_route", model.Fail, fmt.Sprintf("Expected upstream %s; loaded %v", s.Proxy.Upstream, upstreams)))
	}
	return append(checks, check("proxy_route", model.Pass, "Expected domain present in loaded routes"))
}

func stringTrim(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}
