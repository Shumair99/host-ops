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
	sources := map[string]Source{}
	for _, source := range registry.Snapshot.Sources {
		sources[source.ID] = source
	}
	out := []config.Site{}
	for _, item := range registry.Sites {
		if item.State == "removed" {
			continue
		}
		s := config.Site{ID: item.ID, HostID: base.Host.ID, Name: item.Domain, Discovered: true, Runtime: config.Runtime{Type: "unresolved"}, Proxy: config.Proxy{Domain: item.Domain}, Health: config.Health{Path: "/", TimeoutSeconds: 5, Automatic: true}}
		dependencies := map[string]bool{}
		addWorkload := func(id string) {
			if w, ok := workloads[id]; ok {
				for _, key := range []string{w.ID, w.Type, w.Name, w.ProxyKind} {
					if key != "" {
						dependencies[key] = true
					}
				}
				for network := range w.Networks {
					dependencies[network] = true
					dependencies["network:"+network] = true
					dependencies[strings.TrimPrefix(strings.TrimPrefix(network, "docker:"), "podman:")] = true
				}
				for _, mount := range w.Mounts {
					dependencies["storage:"+mount.Source] = true
				}
				for _, connection := range registry.Snapshot.Connections {
					if connection.From == id && connection.Kind == "peer" {
						address := net.JoinHostPort(connection.Address, strconv.Itoa(connection.Port))
						dependencies[address] = true
						dependencies["endpoint:"+address] = true
					}
				}
			}
		}
		for _, id := range WorkloadDependencies(registry.Snapshot, item.WorkloadIDs) {
			addWorkload(id)
		}
		for _, route := range item.Routes {
			addWorkload(route.ProxyID)
			dependencies[route.SourceID] = true
			if kind := sources[route.SourceID].Kind; kind != "" {
				dependencies[kind] = true
			}
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
		if len(item.WorkloadIDs) == 1 {
			// Runtime resolution remains independent of redirect presentation.
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
		redirectTarget := ""
		redirectOnly := len(item.Routes) > 0
		for _, route := range item.Routes {
			if route.Kind != "redirect" || len(route.Paths) > 0 || route.Note != "" {
				redirectOnly = false
				break
			}
			target := redirectHost(route.RedirectTo)
			if target == "" {
				redirectOnly = false
				break
			}
			if target == item.Domain {
				continue
			}
			if redirectTarget != "" && redirectTarget != target {
				redirectOnly = false
				break
			}
			redirectTarget = target
		}
		if redirectOnly {
			s.Proxy.RedirectTo = redirectTarget
		}
		if old, ok := manual[item.Domain]; ok {
			s.ID, s.Name, s.Repository, s.Branch, s.Business, s.Recovery = old.ID, old.Name, old.Repository, old.Branch, old.Business, old.Recovery
			derivedURL := s.Health.PublicURL
			s.Health = old.Health
			if s.Health.PublicURL == "" {
				s.Health.PublicURL = derivedURL
			}
			delete(manual, item.Domain)
		}
		for key := range dependencies {
			s.Dependencies = append(s.Dependencies, key)
		}
		sort.Strings(s.Dependencies)
		out = append(out, s)
	}
	if !base.Discovering() {
		for _, s := range manual {
			out = append(out, s)
		}
	}
	// A redirect has no separate application runtime, but visitors still depend
	// on its destination. Follow only explicit, inventoried redirect targets.
	byDomain := map[string]config.Site{}
	for _, site := range out {
		byDomain[site.Proxy.Domain] = site
	}
	for i := range out {
		dependencies := map[string]bool{}
		for _, dep := range out[i].Dependencies {
			dependencies[dep] = true
		}
		seen := map[string]bool{out[i].Proxy.Domain: true}
		target := out[i].Proxy.RedirectTo
		for target != "" && !seen[target] {
			seen[target] = true
			site, exists := byDomain[target]
			if !exists {
				break
			}
			dependencies[site.ID], dependencies[site.Proxy.Domain] = true, true
			for _, dep := range site.Dependencies {
				dependencies[dep] = true
			}
			target = site.Proxy.RedirectTo
		}
		out[i].Dependencies = out[i].Dependencies[:0]
		for dep := range dependencies {
			out[i].Dependencies = append(out[i].Dependencies, dep)
		}
		sort.Strings(out[i].Dependencies)
	}
	base.Sites = config.OrderedSites(out)
	return base
}

func redirectHost(address string) string {
	address = strings.ReplaceAll(address, "{http.request.uri}", "/")
	address = strings.ReplaceAll(address, "{uri}", "/")
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
