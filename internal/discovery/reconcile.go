package discovery

import (
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Reconcile is the only place observations become sites. Failed reads preserve
// prior facts, while two complete scans over at least 30 seconds confirm removal.
func Reconcile(next Snapshot, previous Registry) Registry {
	current := map[string]Source{}
	for _, source := range next.Sources {
		current[source.ID] = source
	}
	for _, old := range previous.Snapshot.Sources {
		if _, ok := current[old.ID]; ok {
			continue
		}
		parent, ok := current[old.ParentID]
		old.ObservedAt = next.ObservedAt
		if ok && old.ParentID != "linux/processes" && (parent.State == "ready" || parent.State == "absent") {
			old.State, old.Issue = "absent", "Source no longer present in a complete parent inventory"
		} else {
			old.State, old.Issue = "unavailable", "Source not observed in this scan"
		}
		next.Sources = append(next.Sources, old)
		current[old.ID] = old
	}
	for _, workload := range previous.Snapshot.Workloads {
		if source := current[workload.SourceID]; source.State == "unavailable" {
			found := false
			for _, fresh := range next.Workloads {
				if fresh.ID == workload.ID {
					found = true
				}
			}
			if !found {
				next.Workloads = append(next.Workloads, workload)
			}
		}
	}
	for _, route := range previous.Snapshot.Routes {
		if current[route.SourceID].State != "unavailable" {
			continue
		}
		found := false
		for _, fresh := range next.Routes {
			if fresh.ID == route.ID {
				found = true
			}
		}
		if !found {
			next.Routes = append(next.Routes, route)
		}
	}
	activeProxies := map[string]bool{}
	for _, source := range next.Sources {
		if source.ProxyID != "" && source.State == "ready" && source.Mode == "active" {
			activeProxies[source.ProxyID] = true
		}
	}
	for _, route := range next.Routes {
		if s := current[route.SourceID]; route.ProxyID != "" && s.State == "ready" && s.Mode == "active" {
			activeProxies[route.ProxyID] = true
		}
	}
	byDomain := map[string][]Route{}
	patterns := []Route{}
	for _, route := range next.Routes {
		if activeProxies[route.ProxyID] && current[route.SourceID].Mode == "configured" {
			continue
		}
		for _, raw := range route.Hosts {
			domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
			if !ConcreteHost(domain) {
				patterns = append(patterns, route)
				continue
			}
			byDomain[domain] = append(byDomain[domain], route)
		}
	}
	oldSites := map[string]Site{}
	for _, site := range previous.Sites {
		oldSites[site.Domain] = site
	}
	result := Registry{Snapshot: next, Sites: []Site{}, Patterns: patterns}
	for domain, routes := range byDomain {
		site := Site{ID: StableID("site", next.HostID+"|"+domain), Domain: domain, Routes: routes, FirstSeen: next.ObservedAt, LastSeen: next.ObservedAt, State: "configured", WorkloadIDs: []string{}}
		if old, ok := oldSites[domain]; ok {
			site.ID, site.FirstSeen = old.ID, old.FirstSeen
		}
		seen := map[string]bool{}
		fresh, active, unresolved := false, false, false
		for _, route := range routes {
			s := current[route.SourceID]
			fresh = fresh || s.State == "ready"
			active = active || s.State == "ready" && s.Mode == "active"
			unresolved = unresolved || route.Kind == "opaque" || route.Kind == "proxy" && (len(route.Upstreams) == 0 || strings.Contains(route.Note, "Dynamic upstream"))
			for _, upstream := range route.Upstreams {
				ids := ResolveUpstream(upstream, route, next)
				if len(ids) == 0 {
					unresolved = true
				}
				for _, id := range ids {
					seen[id] = true
				}
			}
		}
		if active {
			site.State = "active"
		}
		if unresolved {
			site.State = "unresolved"
		}
		if !fresh {
			site.State = "stale"
			if old, ok := oldSites[domain]; ok {
				site.LastSeen = old.LastSeen
			}
		}
		for id := range seen {
			site.WorkloadIDs = append(site.WorkloadIDs, id)
		}
		sort.Strings(site.WorkloadIDs)
		result.Sites = append(result.Sites, site)
		delete(oldSites, domain)
	}
	for _, site := range oldSites {
		complete := len(site.Routes) > 0
		for _, route := range site.Routes {
			s, ok := current[route.SourceID]
			complete = complete && ok && (s.State == "ready" || s.State == "absent")
		}
		if complete {
			if site.MissingSince.IsZero() {
				site.MissingSince = next.ObservedAt
			}
			site.MissingScans++
			if site.MissingScans >= 2 && next.ObservedAt.Sub(site.MissingSince) >= 30*time.Second {
				site.State = "removed"
			} else {
				site.State = "stale"
			}
		} else {
			site.State = "stale"
		}
		result.Sites = append(result.Sites, site)
	}
	sort.Slice(result.Sites, func(i, j int) bool { return result.Sites[i].Domain < result.Sites[j].Domain })
	return result
}

func ConcreteHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 || strings.ContainsAny(host, "*~{}[]()\\/: \t\r\n") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func ResolveUpstream(upstream string, route Route, snapshot Snapshot) []string {
	if id := route.ResolvedUpstreams[upstream]; id != "" {
		for _, w := range snapshot.Workloads {
			if w.ID == id {
				return []string{id}
			}
		}
	}
	address := upstream
	if strings.HasPrefix(address, "unix:") {
		address = strings.TrimPrefix(address, "unix:")
	}
	if strings.HasPrefix(address, "unix/") {
		address = strings.TrimPrefix(address, "unix/")
	}
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		address = u.Host
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ids := map[string]bool{}
	identities := map[string]bool{}
	for _, w := range snapshot.Workloads {
		for network, details := range w.Networks {
			shared := false
			for _, n := range route.Networks {
				if n == network {
					shared = true
				}
			}
			if len(route.Networks) == 0 && net.ParseIP(host) != nil && host == details.IP {
				shared = true
			}
			if !shared {
				continue
			}
			matched := host == w.Name || host == details.IP
			if matched {
				identities[w.ID] = true
			}
			for _, alias := range details.Aliases {
				matched = matched || host == alias
			}
			if matched {
				ids[w.ID] = true
			}
		}
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			if len(route.Networks) > 0 {
				continue
			} // Container loopback belongs to its own namespace.
			for _, bindings := range w.Ports {
				for _, binding := range bindings {
					_, p, _ := net.SplitHostPort(binding)
					if p == port {
						ids[w.ID] = true
					}
				}
			}
		}
	}
	for _, listener := range snapshot.Listeners {
		if len(route.Networks) > 0 {
			continue
		}
		_, p, _ := net.SplitHostPort(listener.Address)
		if listener.Address != address && !(port != "" && p == port && (host == "127.0.0.1" || host == "localhost" || host == "::1")) {
			continue
		}
		for _, w := range snapshot.Workloads {
			if listener.Unit != "" && w.Name == listener.Unit || listener.PID > 0 && w.PID == listener.PID {
				ids[w.ID] = true
			}
		}
	}
	out := []string{}
	for id := range ids {
		out = append(out, id)
	}
	// An observed address/container identity is stronger than a generic alias.
	// Generic aliases must be unique across every network visible to this proxy.
	if len(identities) > 0 {
		out = nil
		for id := range identities {
			out = append(out, id)
		}
	}
	if len(out) != 1 {
		return []string{}
	}
	sort.Strings(out)
	return out
}
