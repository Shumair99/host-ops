package discovery

import (
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/Shumair99/host-ops/internal/config"
)

// ConfigSites adapts the common inventory for existing operations features.
// Explicit settings override health/business preferences, never observations.
func ConfigSites(registry Registry, base config.Config) config.Config {
	base.Sites = append([]config.Site(nil), base.Sites...)
	manual := map[string]config.Site{}
	for _, s := range base.Sites {
		manual[strings.ToLower(s.Proxy.Domain)] = s
	}
	workloads := map[string]Workload{}
	for _, w := range registry.Snapshot.Workloads {
		workloads[w.ID] = w
	}
	out := []config.Site{}
	for _, item := range registry.Sites {
		if item.State == "removed" {
			continue
		}
		s := config.Site{ID: item.ID, HostID: base.Host.ID, Name: item.Domain, Discovered: true, Runtime: config.Runtime{Type: "unresolved"}, Proxy: config.Proxy{Domain: item.Domain}, Health: config.Health{Path: "/", TimeoutSeconds: 5, Automatic: true}}
		for _, route := range item.Routes {
			if route.Scheme == "https" || s.Health.PublicURL == "" {
				u := url.URL{Scheme: route.Scheme, Host: item.Domain, Path: "/"}
				if route.Port != "" && route.Port != "80" && route.Port != "443" {
					u.Host = net.JoinHostPort(item.Domain, route.Port)
				}
				if net.ParseIP(item.Domain) != nil && strings.Contains(item.Domain, ":") && !strings.HasPrefix(u.Host, "[") {
					u.Host = "[" + item.Domain + "]"
				}
				s.Health.PublicURL = u.String()
			}
			if len(route.Upstreams) > 0 && s.Proxy.Upstream == "" {
				s.Proxy.Upstream = route.Upstreams[0]
			}
			if route.Kind == "static" && route.Root != "" && route.ProxyID == "" {
				s.Runtime = config.Runtime{Type: "static", Directory: route.Root}
			}
		}
		if len(item.WorkloadIDs) > 0 {
			w := workloads[item.WorkloadIDs[0]]
			s.Runtime.Type = w.Type
			if w.Type == "docker" || w.Type == "podman" {
				s.Runtime.Container = w.Name
			}
			if w.Type == "systemd" {
				s.Runtime.Unit = w.Name
			}
			for _, route := range item.Routes {
				for _, network := range route.Networks {
					if _, ok := w.Networks[network]; ok {
						s.Runtime.Network = strings.TrimPrefix(strings.TrimPrefix(network, "docker:"), "podman:")
					}
				}
			}
			_, port, _ := net.SplitHostPort(s.Proxy.Upstream)
			s.Runtime.Port, _ = strconv.Atoi(port)
		}
		if old, ok := manual[item.Domain]; ok {
			s.ID, s.Name, s.Repository, s.Branch, s.Business, s.Recovery = old.ID, old.Name, old.Repository, old.Branch, old.Business, old.Recovery
			s.Health = old.Health
			delete(manual, item.Domain)
		}
		out = append(out, s)
	}
	if !base.Discovering() {
		for _, s := range manual {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	base.Sites = out
	return base
}
