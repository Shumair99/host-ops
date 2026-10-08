package collector

import (
	"context"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
	"sort"
	"strings"
)

type proxyInstance struct {
	kind, binary, engine, container string
	args                            []string
	workload                        discovery.Workload
}

func (i proxyInstance) run(ctx context.Context, runner platform.Runner, name string, args ...string) ([]byte, error) {
	if i.engine != "" {
		return runner.Run(ctx, i.engine, append([]string{"exec", i.container, name}, args...)...)
	}
	return runner.Run(ctx, name, args...)
}
func argument(args []string, key, defaultValue string) string {
	for idx, value := range args {
		if value == key && idx+1 < len(args) {
			return args[idx+1]
		}
		if strings.HasPrefix(value, key+"=") {
			return strings.TrimPrefix(value, key+"=")
		}
	}
	return defaultValue
}

func readCaddy(ctx context.Context, runner platform.Runner, host config.Host, instance proxyInstance, snapshot *discovery.Snapshot) {
	networks := []string{}
	for name := range instance.workload.Networks {
		networks = append(networks, name)
	}
	sort.Strings(networks)
	active := discovery.Source{ID: "active/caddy/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "caddy", State: "ready", Mode: "active", ObservedAt: snapshot.ObservedAt}
	active.ProxyID = instance.workload.ID
	admin := "http://127.0.0.1:2019"
	if host.CaddyAdminURL != "" && (host.CaddyContainer == "" || host.CaddyContainer == instance.container) {
		admin = host.CaddyAdminURL
	}
	var data []byte
	var err error
	if instance.engine != "" {
		data, err = instance.run(ctx, runner, "wget", "-qO-", admin+"/config/apps/http/servers/")
	} else {
		data, err = runner.Run(ctx, "curl", "--silent", "--fail", "--max-time", "5", admin+"/config/apps/http/servers/")
	}
	if err != nil {
		active.State, active.Issue = "unavailable", "Active Caddy configuration API unreadable"
	} else {
		routes, parseErr := discovery.ParseCaddy(data, active.ID, instance.workload.ID, networks)
		if parseErr != nil {
			active.State, active.Issue = "unavailable", parseErr.Error()
		} else {
			snapshot.Routes = append(snapshot.Routes, routes...)
		}
	}
	snapshot.Sources = append(snapshot.Sources, active)
	declared := discovery.Source{ID: "configured/caddy/" + instance.workload.ID, ParentID: instance.workload.SourceID, Kind: "caddy", State: "ready", Mode: "configured", ObservedAt: snapshot.ObservedAt}
	file := argument(instance.args, "--config", "")
	adapter := argument(instance.args, "--adapter", "")
	if file == "" {
		declared.State, declared.Issue = "unavailable", "Caddy launch configuration path not exposed"
		snapshot.Sources = append(snapshot.Sources, declared)
		return
	}
	if adapter == "" && strings.HasSuffix(file, "Caddyfile") {
		adapter = "caddyfile"
	}
	if adapter == "" {
		data, err = instance.run(ctx, runner, "cat", "--", file)
	} else {
		binary := instance.binary
		if binary == "" {
			binary = "caddy"
		}
		data, err = instance.run(ctx, runner, binary, "adapt", "--config", file, "--adapter", adapter)
	}
	if err != nil {
		declared.State, declared.Issue = "unavailable", "Declared Caddy configuration unreadable"
	} else {
		routes, parseErr := discovery.ParseCaddy(data, declared.ID, instance.workload.ID, networks)
		if parseErr != nil {
			declared.State, declared.Issue = "unavailable", parseErr.Error()
		} else {
			snapshot.Routes = append(snapshot.Routes, routes...)
		}
	}
	snapshot.Sources = append(snapshot.Sources, declared)
}
