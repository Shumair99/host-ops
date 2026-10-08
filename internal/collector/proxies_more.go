package collector

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"time"
)

func proxyNetworks(instance proxyInstance) []string {
	out := []string{}
	for name := range instance.workload.Networks {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func readNginx(ctx context.Context, runner platform.Runner, instance proxyInstance, snapshot *discovery.Snapshot) {
	source := discovery.Source{ID: "configured/nginx/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "nginx", State: "ready", Mode: "configured", ObservedAt: snapshot.ObservedAt}
	args := []string{"-T"}
	if file := argument(instance.args, "-c", ""); file != "" {
		args = append(args, "-c", file)
	}
	if prefix := argument(instance.args, "-p", ""); prefix != "" {
		args = append(args, "-p", prefix)
	}
	binary := instance.binary
	if binary == "" {
		binary = "nginx"
	}
	data, err := instance.run(ctx, runner, binary, args...)
	if err != nil {
		source.State, source.Issue = "unavailable", "Nginx configuration dump unreadable"
	} else {
		routes, err := discovery.ParseNginx(data, source.ID, instance.workload.ID, proxyNetworks(instance))
		if err != nil {
			source.State, source.Issue = "unavailable", err.Error()
		} else {
			snapshot.Routes = append(snapshot.Routes, routes...)
		}
	}
	snapshot.Sources = append(snapshot.Sources, source)
}

func readTraefik(ctx context.Context, instance proxyInstance, metadata []containerMeta, snapshot *discovery.Snapshot) {
	networks := proxyNetworks(instance)
	declared := discovery.Source{ID: "configured/traefik/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "traefik", State: "ready", Mode: "configured", ObservedAt: snapshot.ObservedAt, Issue: "Label routes are declared; provider constraints and loaded state require the API"}
	for _, meta := range metadata {
		if meta.Engine != instance.engine {
			continue
		}
		snapshot.Routes = append(snapshot.Routes, discovery.TraefikLabels(meta.Config.Labels, declared.ID, instance.workload.ID, meta.Name, networks)...)
	}
	snapshot.Sources = append(snapshot.Sources, declared)
	active := discovery.Source{ID: "active/traefik/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "traefik", State: "unavailable", Mode: "active", ObservedAt: snapshot.ObservedAt, Issue: "Traefik API is not exposed by this installation"}
	active.ProxyID = instance.workload.ID
	if argument(instance.args, "--api.insecure", "") != "true" {
		snapshot.Sources = append(snapshot.Sources, active)
		return
	}
	address := argument(instance.args, "--entrypoints.traefik.address", ":8080")
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		port = "8080"
	}
	host := "127.0.0.1"
	if instance.engine != "" {
		for _, network := range networks {
			if ip := instance.workload.Networks[network].IP; ip != "" {
				host = ip
				break
			}
		}
	}
	data, err := readJSON(ctx, "http://"+net.JoinHostPort(host, port)+"/api/rawdata")
	if err == nil {
		routes, parseErr := discovery.ParseTraefik(data, active.ID, instance.workload.ID, networks)
		if parseErr == nil {
			active.State, active.Issue = "ready", ""
			snapshot.Routes = append(snapshot.Routes, routes...)
		} else {
			active.Issue = parseErr.Error()
		}
	}
	snapshot.Sources = append(snapshot.Sources, active)
}

func readJSON(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("configuration API unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("configuration API denied")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return nil, fmt.Errorf("configuration API output exceeds limit")
	}
	return data, nil
}

func readApache(ctx context.Context, runner platform.Runner, instance proxyInstance, snapshot *discovery.Snapshot) {
	source := discovery.Source{ID: "configured/apache/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "apache", State: "ready", Mode: "configured", ObservedAt: snapshot.ObservedAt, Issue: "Explicit virtual hosts are supported; conditional rewrites are not expanded"}
	binary := instance.binary
	if instance.engine != "" {
		binary = "httpd"
	}
	args := []string{"-S"}
	if file := argument(instance.args, "-f", ""); file != "" {
		args = append(args, "-f", file)
	}
	data, err := instance.run(ctx, runner, binary, args...)
	if err != nil {
		source.State, source.Issue = "unavailable", "Apache virtual-host dump unreadable"
		snapshot.Sources = append(snapshot.Sources, source)
		return
	}
	files := map[string]bool{}
	for _, match := range regexp.MustCompile(`\((/[^\r\n()]+):\d+\)`).FindAllStringSubmatch(string(data), -1) {
		files[match[1]] = true
	}
	if len(files) == 0 {
		source.State, source.Issue = "unavailable", "Virtual-host configuration files not exposed in dump"
	}
	for file := range files {
		data, err = instance.run(ctx, runner, "cat", "--", file)
		if err != nil {
			source.State, source.Issue = "unavailable", "Virtual-host configuration read incomplete"
			continue
		}
		snapshot.Routes = append(snapshot.Routes, discovery.ParseApache(data, source.ID, instance.workload.ID, proxyNetworks(instance))...)
	}
	snapshot.Sources = append(snapshot.Sources, source)
}
