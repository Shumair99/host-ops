package discovery

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
)

type caddyScope struct {
	hosts, paths             []string
	root, scheme, port, note string
}

func ParseCaddy(data []byte, source, proxy string, networks []string) ([]Route, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("invalid Caddy JSON")
	}
	if apps, ok := root["apps"].(map[string]any); ok {
		http, _ := apps["http"].(map[string]any)
		root, _ = http["servers"].(map[string]any)
	}
	if servers, ok := root["servers"].(map[string]any); ok {
		root = servers
	}
	result := []Route{}
	var handlers func([]any, caddyScope)
	var routes func([]any, caddyScope)
	appendRoute := func(scope caddyScope, kind string, upstreams []string, redirect string) {
		hosts := scope.hosts
		if len(hosts) == 0 {
			hosts = []string{"*"}
		}
		r := Route{SourceID: source, ProxyID: proxy, Hosts: hosts, Paths: scope.paths, Kind: kind, Scheme: scope.scheme, Port: scope.port, Root: scope.root, Upstreams: upstreams, RedirectTo: redirect, Networks: networks, Note: scope.note}
		key, _ := json.Marshal(r)
		r.ID = StableID("route", string(key))
		result = append(result, r)
	}
	handlers = func(list []any, scope caddyScope) {
		for _, value := range list {
			h, ok := value.(map[string]any)
			if !ok {
				continue
			}
			switch h["handler"] {
			case "vars":
				if value, ok := h["root"].(string); ok {
					scope.root = value
				}
			case "subroute":
				child, _ := h["routes"].([]any)
				routes(child, scope)
			case "reverse_proxy":
				upstreams := []string{}
				for _, value := range array(h["upstreams"]) {
					u, _ := value.(map[string]any)
					if dial, ok := u["dial"].(string); ok {
						upstreams = append(upstreams, dial)
					}
				}
				if h["dynamic_upstreams"] != nil {
					scope.note = "Dynamic upstreams require request-time resolution"
				}
				appendRoute(scope, "proxy", upstreams, "")
			case "file_server":
				if value, ok := h["root"].(string); ok {
					scope.root = value
				}
				appendRoute(scope, "static", nil, "")
			case "static_response":
				location := ""
				headers, _ := h["headers"].(map[string]any)
				for _, v := range array(headers["Location"]) {
					if s, ok := v.(string); ok {
						location = s
						break
					}
				}
				kind := "response"
				if location != "" {
					kind = "redirect"
				}
				appendRoute(scope, kind, nil, location)
			case "rewrite", "encode", "headers", "authentication", "request_body", "tracing", "log_append":
			default:
				if h["handler"] != nil {
					scope.note = "Route contains a handler with no workload mapping"
					appendRoute(scope, "opaque", nil, "")
				}
			}
		}
	}
	routes = func(list []any, parent caddyScope) {
		for _, value := range list {
			route, ok := value.(map[string]any)
			if !ok {
				continue
			}
			matches := array(route["match"])
			if len(matches) == 0 {
				matches = []any{map[string]any{}}
			}
			for _, m := range matches {
				match, _ := m.(map[string]any)
				scope := parent
				if hosts := stringArray(match["host"]); len(hosts) > 0 {
					scope.hosts = intersectHosts(parent.hosts, hosts)
					if len(scope.hosts) == 0 {
						continue
					}
				}
				if paths := stringArray(match["path"]); len(paths) > 0 {
					scope.paths = append(append([]string{}, scope.paths...), paths...)
				}
				for k := range match {
					if k != "host" && k != "path" {
						scope.note = "Additional request matchers apply"
					}
				}
				if h, ok := route["handle"].([]any); ok {
					handlers(h, scope)
				}
			}
		}
	}
	names := []string{}
	for key := range root {
		names = append(names, key)
	}
	sort.Strings(names)
	for _, name := range names {
		server, ok := root[name].(map[string]any)
		if !ok {
			continue
		}
		listens := stringArray(server["listen"])
		if len(listens) == 0 {
			listens = []string{":443"}
		}
		for _, listen := range listens {
			_, port, err := net.SplitHostPort(listen)
			if err != nil {
				port = strings.TrimPrefix(listen, ":")
			}
			scheme := "http"
			if port == "443" || server["tls_connection_policies"] != nil {
				scheme = "https"
			}
			routes(array(server["routes"]), caddyScope{scheme: scheme, port: port})
		}
	}
	return result, nil
}

func array(value any) []any { list, _ := value.([]any); return list }
func stringArray(value any) []string {
	out := []string{}
	for _, v := range array(value) {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func intersectHosts(a, b []string) []string {
	if len(a) == 0 {
		return b
	}
	out := []string{}
	for _, left := range a {
		for _, right := range b {
			switch {
			case left == right:
				out = append(out, left)
			case strings.HasPrefix(left, "*.") && strings.HasSuffix(right, left[1:]):
				out = append(out, right)
			case strings.HasPrefix(right, "*.") && strings.HasSuffix(left, right[1:]):
				out = append(out, left)
			}
		}
	}
	return out
}
