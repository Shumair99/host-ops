package preflight

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/capacity"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"github.com/Shumair99/host-ops/internal/recovery"
	runtimecheck "github.com/Shumair99/host-ops/internal/runtime"
	"github.com/Shumair99/host-ops/internal/storage"
	"sort"
	"strings"
	"time"
)

type Impact struct {
	HostID       string              `json:"host_id"`
	Resource     string              `json:"resource"`
	Affected     []config.Site       `json:"affected"`
	Dependencies []string            `json:"dependencies"`
	Notes        []string            `json:"notes"`
	Graph        *discovery.Topology `json:"graph,omitempty"`
}

func DependencyInventory(c config.Config, resource string, reg discovery.Registry) (Impact, error) {
	r, err := Dependency(c, resource)
	g := discovery.Graph(reg)
	r.Graph = &g
	if err != nil {
		for _, n := range g.Nodes {
			if n.ID == resource || n.Label == resource {
				return r, nil
			}
		}
	}
	return r, err
}

func Dependency(c config.Config, resource string) (Impact, error) {
	r := Impact{HostID: c.Host.ID, Resource: resource, Affected: []config.Site{}, Dependencies: []string{}, Notes: []string{"This is a read-only simulation from observed or configured dependencies; no failure is induced.", "Unresolved routes and hidden application dependencies may expand the actual blast radius."}}
	known := false
	dependencies := map[string]bool{"host:" + c.Host.ID: true}
	for _, s := range c.Sites {
		affects := resource == "host" || resource == c.Host.ID || resource == "host:"+c.Host.ID || resource == s.ID || resource == s.Runtime.Container || resource == s.Runtime.Unit || resource == s.Runtime.Directory || resource == s.Proxy.Domain
		if s.Discovered {
			for _, dependency := range s.Dependencies {
				affects = affects || dependency == resource
			}
		} else {
			if resource == "caddy" || c.Host.CaddyContainer != "" && resource == c.Host.CaddyContainer {
				affects = s.Proxy.Domain != ""
			}
			if resource == "docker" {
				affects = s.Runtime.Type == "docker" || c.Host.CaddyContainer != "" && s.Proxy.Domain != ""
			}
		}
		if affects {
			known = true
			r.Affected = append(r.Affected, s)
			for _, dependency := range s.Dependencies {
				dependencies[dependency] = true
			}
		}
	}
	if !known {
		return r, fmt.Errorf("resource is not registered in the dependency graph")
	}
	if c.Host.CaddyContainer != "" {
		for _, site := range r.Affected {
			if !site.Discovered {
				dependencies["docker"], dependencies["caddy:"+c.Host.CaddyContainer] = true, true
			}
		}
	}
	for dependency := range dependencies {
		r.Dependencies = append(r.Dependencies, dependency)
	}
	sort.Strings(r.Dependencies)
	return r, nil
}

type Report struct {
	HostID         string        `json:"host_id"`
	Target         string        `json:"target"`
	ObservedAt     time.Time     `json:"observed_at"`
	Overall        string        `json:"overall"`
	Recommendation string        `json:"recommendation"`
	Checks         []model.Check `json:"checks"`
	Impact         Impact        `json:"impact"`
}

func Recommendation(checks []model.Check) string {
	overall := model.Overall(checks)
	if overall == "critical" {
		return "Do not proceed: confirmed blocking checks failed."
	}
	if overall != "healthy" {
		return "Review before proceeding: warning or unverified safety evidence remains."
	}
	return "Observed preflight checks passed. Confirm the maintenance window and independent monitoring before proceeding."
}
func Assess(ctx context.Context, c config.Config, target string, reports []model.Report, store *storage.Store, runner platform.Runner) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resource := target
	if target == "reboot" {
		resource = "host"
	}
	impact, err := Dependency(c, resource)
	if c.Discovering() && store != nil {
		if reg, e := store.Inventory(ctx, c.Host.ID); e == nil && len(reg.Snapshot.Workloads) > 0 {
			impact, err = DependencyInventory(c, resource, reg)
		}
	}
	if err != nil {
		return Report{}, err
	}
	r := Report{HostID: c.Host.ID, Target: target, ObservedAt: time.Now().UTC(), Checks: []model.Check{}, Impact: impact}
	add := func(site, layer string, state model.State, summary string) {
		r.Checks = append(r.Checks, model.Check{HostID: c.Host.ID, SiteID: site, Layer: layer, State: state, Summary: summary})
	}
	if target == "reboot" {
		data, err := runner.Run(ctx, "systemctl", "is-enabled", "docker")
		state, summary := model.Pass, "Docker enabled at boot"
		if err != nil {
			state = model.Unknown
			summary = "Docker boot enablement unavailable"
		} else if strings.TrimSpace(string(data)) != "enabled" {
			state = model.Fail
			summary = "Docker unit not enabled at boot"
		}
		add("", "docker_boot", state, summary)
		if c.Host.CaddyContainer != "" {
			site := config.Site{HostID: c.Host.ID, Runtime: config.Runtime{Type: "docker", Container: c.Host.CaddyContainer}}
			runtime, checks := runtimecheck.InspectDocker(ctx, runner, site)
			state := model.Unknown
			summary := "Caddy restart behaviour unavailable"
			if runtime.Status != "" {
				state = model.Pass
				summary = "Caddy: " + runtime.Status + "; restart policy: " + runtime.RestartPolicy
				if runtime.Status != "running" || runtime.RestartPolicy != "always" && runtime.RestartPolicy != "unless-stopped" {
					state = model.Fail
				}
			}
			for _, check := range checks {
				if check.State == model.Unknown {
					state = model.Unknown
					summary = check.Summary
				}
			}
			add("", "caddy_boot", state, summary)
		}
	}
	byID := map[string]model.Report{}
	for _, report := range reports {
		byID[report.SiteID] = report
	}
	for _, site := range impact.Affected {
		current := byID[site.ID]
		state := model.Unknown
		summary := "Fresh health observation missing"
		if model.Fresh(current.ObservedAt, time.Now(), model.DefaultMaxAge) {
			summary = current.Overall
			state = model.Pass
			if current.Overall == "critical" {
				state = model.Fail
			} else if current.Overall != "healthy" {
				state = model.Warn
			}
		}
		add(site.ID, "site_health", state, summary)
		if site.Runtime.Type == "docker" {
			restart := model.Unknown
			if current.Runtime.Status != "" && model.Fresh(current.ObservedAt, time.Now(), model.DefaultMaxAge) {
				restart = model.Fail
				if current.Runtime.Status == "running" && (current.Runtime.RestartPolicy == "always" || current.Runtime.RestartPolicy == "unless-stopped") {
					restart = model.Pass
				}
			}
			add(site.ID, "restart_policy", restart, current.Runtime.RestartPolicy)
		}
		recovery, err := recovery.Assess(ctx, c, site, store)
		if err != nil {
			return r, err
		}
		add(site.ID, "backup_freshness", recovery.RPOstate, "Backup freshness against configured RPO")
		add(site.ID, "restore_evidence", recovery.RestoreState, "Reported isolated restore evidence")
		if recovery.LastBackup != nil && recovery.LastBackup.Status == "in_progress" {
			add(site.ID, "backup_running", model.Fail, "A backup is reported in progress; verify completion before maintenance")
		}
	}
	capacity, err := capacity.Assess(ctx, c, store)
	if err != nil {
		return r, err
	}
	add("", "host_capacity", capacity.State, "Measured host capacity and disk headroom")
	r.Overall = model.Overall(r.Checks)
	r.Recommendation = Recommendation(r.Checks)
	return r, nil
}
