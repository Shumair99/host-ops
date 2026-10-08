package discovery

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

func safeAddress(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		return u.String()
	}
	return raw
}

func TraefikRule(rule string) (hosts, paths []string, note string) {
	hostPattern := regexp.MustCompile("Host\\(\\s*[`\"']([^`\"']+)[`\"']\\s*\\)")
	for _, match := range hostPattern.FindAllStringSubmatchIndex(rule, -1) {
		prefix := strings.TrimSpace(rule[:match[0]])
		if strings.HasSuffix(prefix, "!") {
			continue
		}
		hosts = append(hosts, strings.ToLower(rule[match[2]:match[3]]))
	}
	for _, match := range regexp.MustCompile("Path(?:Prefix)?\\(\\s*[`\"']([^`\"']+)[`\"']\\s*\\)").FindAllStringSubmatch(rule, -1) {
		paths = append(paths, match[1])
	}
	if len(hosts) == 0 {
		hosts = []string{"*"}
		for _, match := range regexp.MustCompile("HostRegexp\\(\\s*[`\"']([^`\"']+)[`\"']\\s*\\)").FindAllStringSubmatch(rule, -1) {
			hosts = []string{"~" + match[1]}
		}
		note = "Host pattern cannot enumerate individual domains"
	}
	if strings.Contains(rule, "&&") || strings.Contains(rule, "!") {
		note = "Additional request matchers apply"
	}
	return
}

func ParseTraefik(data []byte, source, proxy string, networks []string) ([]Route, error) {
	var root struct {
		Routers map[string]struct {
			Rule, Service, Status string
			EntryPoints           []string
			TLS                   json.RawMessage
		}
		Services map[string]struct {
			LoadBalancer *struct{ Servers []struct{ URL string } }
		}
	}
	if json.Unmarshal(data, &root) != nil {
		return nil, fmt.Errorf("invalid Traefik routing JSON")
	}
	keys := []string{}
	for key := range root.Routers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	routes := []Route{}
	for _, key := range keys {
		router := root.Routers[key]
		if router.Status == "disabled" {
			continue
		}
		hosts, paths, note := TraefikRule(router.Rule)
		scheme, port := "http", "80"
		if len(router.TLS) > 0 && string(router.TLS) != "null" {
			scheme, port = "https", "443"
		}
		ups := []string{}
		service := root.Services[router.Service]
		if service.LoadBalancer != nil {
			for _, server := range service.LoadBalancer.Servers {
				ups = append(ups, safeAddress(server.URL))
			}
		}
		r := Route{ID: StableID("route", source+"|"+key), SourceID: source, ProxyID: proxy, Hosts: hosts, Paths: paths, Kind: "proxy", Scheme: scheme, Port: port, Upstreams: ups, Networks: networks, Note: note}
		routes = append(routes, r)
	}
	return routes, nil
}

func TraefikLabels(labels map[string]string, source, proxy, upstream string, networks []string) []Route {
	if labels["traefik.enable"] == "false" {
		return nil
	}
	routes := []Route{}
	keys := []string{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.http.routers.") && strings.HasSuffix(key, ".rule") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		prefix := strings.TrimSuffix(key, ".rule")
		router := strings.TrimPrefix(prefix, "traefik.http.routers.")
		service := labels[prefix+".service"]
		if service == "" {
			service = router
		}
		port := labels["traefik.http.services."+service+".loadbalancer.server.port"]
		if port == "" {
			port = "?"
		}
		hosts, paths, note := TraefikRule(labels[key])
		scheme, publicPort := "http", "80"
		if labels[prefix+".tls"] == "true" || labels[prefix+".tls.certresolver"] != "" {
			scheme, publicPort = "https", "443"
		}
		r := Route{ID: StableID("route", source+"|"+router), SourceID: source, ProxyID: proxy, Hosts: hosts, Paths: paths, Kind: "proxy", Scheme: scheme, Port: publicPort, Upstreams: []string{upstream + ":" + port}, Networks: networks, Note: note}
		routes = append(routes, r)
	}
	return routes
}
