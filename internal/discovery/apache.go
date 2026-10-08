package discovery

import (
	"encoding/json"
	"net"
	"regexp"
	"strings"
)

func ParseApache(data []byte, source, proxy string, networks []string) []Route {
	pattern := regexp.MustCompile(`(?is)<VirtualHost\s+([^>]+)>(.*?)</VirtualHost\s*>`)
	out := []Route{}
	for _, match := range pattern.FindAllStringSubmatch(string(data), -1) {
		hosts := []string{}
		root, target, redirect := "", "", ""
		for _, line := range strings.Split(match[2], "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "servername":
				hosts = append(hosts, strings.Trim(fields[1], "\""))
			case "serveralias":
				hosts = append(hosts, fields[1:]...)
			case "documentroot":
				root = strings.Trim(strings.Join(fields[1:], " "), "\"")
			case "proxypass":
				if len(fields) > 2 {
					target = fields[2]
				}
			case "redirect":
				redirect = fields[len(fields)-1]
			}
		}
		if len(hosts) == 0 {
			hosts = []string{"*"}
		}
		for _, listen := range strings.Fields(match[1]) {
			_, port, _ := net.SplitHostPort(listen)
			scheme := "http"
			if port == "443" {
				scheme = "https"
			}
			kind := "static"
			ups := []string{}
			if target != "" {
				kind = "proxy"
				ups = []string{safeAddress(target)}
			}
			if redirect != "" {
				kind = "redirect"
			}
			r := Route{SourceID: source, ProxyID: proxy, Hosts: hosts, Scheme: scheme, Port: port, Kind: kind, Root: root, Upstreams: ups, RedirectTo: safeAddress(redirect), Networks: networks, Note: "Declared virtual host; conditional directives may affect routing"}
			key, _ := json.Marshal(r)
			r.ID = StableID("route", string(key))
			out = append(out, r)
		}
	}
	return out
}
