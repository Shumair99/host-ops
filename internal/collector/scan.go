package collector

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

func Scan(ctx context.Context, host config.Host) discovery.Snapshot {
	snapshot := discovery.Snapshot{HostID: host.ID, ObservedAt: time.Now().UTC(), OS: runtime.GOOS, Architecture: runtime.GOARCH, Sources: []discovery.Source{}, Workloads: []discovery.Workload{}, Routes: []discovery.Route{}}
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				snapshot.OS = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "host", Kind: "linux", State: "ready", Mode: "active", ObservedAt: snapshot.ObservedAt})
	ctx, cancel := inspectionContext(ctx)
	defer cancel()
	runner := platform.Command{Host: config.Host{ID: host.ID}}
	metadata := append(readRuntime(ctx, runner, "docker", &snapshot), readRuntime(ctx, runner, "podman", &snapshot)...)
	proxies := []proxyInstance{}
	for _, meta := range metadata {
		for _, w := range snapshot.Workloads {
			if w.InstanceID == meta.ID && w.ProxyKind != "" && w.Status == "running" {
				proxies = append(proxies, proxyInstance{kind: w.ProxyKind, binary: w.ProxyKind, engine: meta.Engine, container: meta.Name, args: meta.Args, workload: w})
				break
			}
		}
	}
	seen := map[string]bool{}
	readServices(ctx, runner, &snapshot)
	for _, process := range readProcesses(ctx, runner, &snapshot) {
		if !seen[process.workload.ID] {
			proxies = append(proxies, proxyInstance{kind: process.kind, binary: process.binary, args: process.args, workload: process.workload})
			seen[process.workload.ID] = true
		}
	}
	for _, proxy := range proxies {
		switch proxy.kind {
		case "caddy":
			readCaddy(ctx, runner, host, proxy, &snapshot)
		case "nginx":
			readNginx(ctx, runner, proxy, &snapshot)
		case "traefik":
			readTraefik(ctx, proxy, metadata, &snapshot)
		case "apache":
			readApache(ctx, runner, proxy, &snapshot)
		default:
			snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "proxy/" + proxy.kind + "/" + proxy.workload.ID, ParentID: proxy.workload.SourceID, Kind: proxy.kind, State: "unavailable", Mode: "configured", ObservedAt: snapshot.ObservedAt, Issue: "Proxy reader is not yet available"})
		}
	}
	resolveRouteDNS(ctx, runner, proxies, &snapshot)
	filterWorkloads(&snapshot, host)
	observeConnections("/proc", &snapshot)
	readBackupArtifacts(ctx, &snapshot)
	return snapshot
}

func filterWorkloads(snapshot *discovery.Snapshot, host config.Host) {
	if host.AllServices {
		return
	}
	ids, units, pids := map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, unit := range host.ServiceUnits {
		units[unit] = true
	}
	unixPaths := map[string]bool{}
	for _, route := range snapshot.Routes {
		ids[route.ProxyID] = true
		for _, upstream := range route.Upstreams {
			for _, id := range discovery.ResolveUpstream(upstream, route, *snapshot) {
				ids[id] = true
			}
			unixPaths[strings.TrimPrefix(strings.TrimPrefix(upstream, "unix:"), "unix/")] = true
		}
	}
	for _, listener := range snapshot.Listeners {
		_, _, err := net.SplitHostPort(listener.Address)
		if err == nil || unixPaths[listener.Address] {
			units[listener.Unit] = true
			if listener.PID > 0 {
				pids[listener.PID] = true
			}
		}
	}
	kept := []discovery.Workload{}
	for _, w := range snapshot.Workloads {
		if w.Type != "systemd" && w.Type != "process" || ids[w.ID] || w.ProxyKind != "" || units[w.Name] || pids[w.PID] {
			kept = append(kept, w)
		}
	}
	snapshot.Workloads = kept
}

func rememberServices(host config.Host, snapshot discovery.Snapshot) config.Host {
	units := map[string]bool{}
	host.ServiceUnits = append([]string(nil), host.ServiceUnits...)
	for _, unit := range host.ServiceUnits {
		units[unit] = true
	}
	for _, w := range snapshot.Workloads {
		if w.Type == "systemd" && !units[w.Name] {
			host.ServiceUnits = append(host.ServiceUnits, w.Name)
			units[w.Name] = true
		}
	}
	return host
}
