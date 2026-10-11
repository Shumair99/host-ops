package collector

import (
	"context"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"net"
	"net/url"
	"strings"
)

func dnsAnswers(data, hostname string) []string {
	result := []string{}
	named := false
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[0] == "Name:" {
			named = strings.TrimSuffix(f[1], ".") == hostname
			continue
		}
		if named && (f[0] == "Address:" || f[0] == "Address") {
			if ip := net.ParseIP(f[len(f)-1]); ip != nil {
				result = append(result, ip.String())
			}
		}
	}
	return result
}

// Resolve ambiguous aliases from the proxy's own DNS namespace and require
// one inspected IP on its actual networks. Never choose the first alias.
func resolveRouteDNS(ctx context.Context, runner platform.Runner, proxies []proxyInstance, snapshot *discovery.Snapshot) {
	cache := map[string][]string{}
	for i := range snapshot.Routes {
		route := &snapshot.Routes[i]
		for _, upstream := range route.Upstreams {
			if len(discovery.ResolveUpstream(upstream, *route, *snapshot)) > 0 {
				continue
			}
			address := upstream
			if u, err := url.Parse(address); err == nil && u.Host != "" {
				address = u.Host
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			if net.ParseIP(host) != nil || !discovery.ConcreteHost(host) {
				continue
			}
			for _, proxy := range proxies {
				if proxy.workload.ID != route.ProxyID || proxy.engine == "" {
					continue
				}
				key := proxy.workload.ID + "|" + host
				ips, exists := cache[key]
				if !exists {
					if len(cache) >= 32 {
						continue
					}
					data, e := proxy.run(ctx, runner, "nslookup", host)
					if e == nil {
						ips = dnsAnswers(string(data), host)
					}
					cache[key] = ips
				}
				matches := map[string]bool{}
				for _, w := range snapshot.Workloads {
					for network, details := range w.Networks {
						if _, shared := proxy.workload.Networks[network]; !shared {
							continue
						}
						for _, ip := range ips {
							if details.IP == ip {
								matches[w.ID] = true
							}
						}
					}
				}
				if len(matches) == 1 {
					if route.ResolvedUpstreams == nil {
						route.ResolvedUpstreams = map[string]string{}
					}
					for id := range matches {
						route.ResolvedUpstreams[upstream] = id
					}
				}
			}
		}
	}
}
