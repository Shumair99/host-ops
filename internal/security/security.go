package security

import (
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	runtimecheck "github.com/Shumair99/host-ops/internal/runtime"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Finding struct {
	SiteID        string      `json:"site_id"`
	SiteIDs       []string    `json:"site_ids,omitempty"`
	Container     string      `json:"container,omitempty"`
	State         model.State `json:"state"`
	Title         string      `json:"title"`
	Evidence      string      `json:"evidence"`
	Investigation string      `json:"investigation"`
}
type Container struct {
	Name   string `json:"name"`
	ID     string `json:"id"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Ports  string `json:"ports"`
	SiteID string `json:"site_id,omitempty"`
}
type Exposure struct {
	SiteID        string      `json:"site_id"`
	Container     string      `json:"container"`
	HostIP        string      `json:"host_ip"`
	HostPort      string      `json:"host_port"`
	ContainerPort string      `json:"container_port"`
	State         model.State `json:"state"`
	Evidence      string      `json:"evidence"`
}
type Report struct {
	HostID          string      `json:"host_id"`
	ObservedAt      time.Time   `json:"observed_at"`
	Findings        []Finding   `json:"findings"`
	Exposures       []Exposure  `json:"exposures"`
	Containers      []Container `json:"containers"`
	PossibleOrphans []Container `json:"possible_orphans"`
	Listening       []string    `json:"listening"`
	Unavailable     []string    `json:"unavailable"`
}

func Posture(site config.Site, r model.Report) []Finding {
	if site.Runtime.Type != "docker" {
		for _, c := range r.Checks {
			if c.Layer == "runtime" && c.State == model.Skip {
				return []Finding{{SiteID: site.ID, State: model.Skip, Title: "No separate application container", Evidence: c.Summary}}
			}
		}
		state := model.Skip
		if site.Runtime.Type == "unresolved" || site.Runtime.Type == "" || site.Runtime.Type == "external" {
			state = model.Unknown
		}
		return []Finding{{SiteID: site.ID, State: state, Title: "Container posture unavailable for this runtime", Evidence: "Runtime: " + site.Runtime.Type, Investigation: "Assess permissions and exposure for this runtime separately."}}
	}
	if r.Runtime.Status == "" || !model.Fresh(r.ObservedAt, time.Now(), model.DefaultMaxAge) {
		return []Finding{{SiteID: site.ID, State: model.Unknown, Title: "Fresh runtime metadata unavailable", Investigation: "Collect runtime state before evaluating security posture."}}
	}
	if !r.Runtime.PostureObserved {
		return []Finding{{SiteID: site.ID, State: model.Unknown, Title: "Container hardening settings not observed", Investigation: "Refresh discovery or inspect this container before evaluating its permissions and limits."}}
	}
	out := []Finding{}
	add := func(condition bool, title, evidence, investigation string, severity model.State) {
		state := model.Pass
		if condition {
			state = severity
		}
		out = append(out, Finding{SiteID: site.ID, State: state, Title: title, Evidence: evidence, Investigation: investigation})
	}
	permissionState, permissionEvidence := Permissions(r.Runtime)
	out = append(out, Finding{SiteID: site.ID, State: permissionState, Title: "Observed process permissions", Evidence: permissionEvidence, Investigation: "For root application processes, test a supported non-root image with the required ports and writable paths."})
	add(r.Runtime.Privileged, "Privileged execution", "Docker privileged flag", "Remove unnecessary privileged execution after verifying dependencies.", model.Fail)
	memoryEvidence := "Configured bytes: " + strconv.FormatInt(r.Runtime.MemoryLimit, 10)
	if r.Runtime.MemoryLimit == 0 {
		memoryEvidence = "No container memory cap; a runaway process can exhaust shared VPS memory."
	}
	add(r.Runtime.MemoryLimit == 0, "Memory limit / availability", memoryEvidence, "Set mem_limit in Compose using measured peak usage and headroom.", model.Warn)
	add(!r.Runtime.ReadOnly, "Writable root filesystem", "Container root filesystem read-only: "+strconv.FormatBool(r.Runtime.ReadOnly), "Test read_only: true with writable volumes or tmpfs for required application paths.", model.Warn)
	last := r.Runtime.Image[strings.LastIndex(r.Runtime.Image, "/")+1:]
	tag := last[strings.LastIndex(last, ":")+1:]
	mutable := !strings.Contains(last, "@") && (tag == "latest" || strings.HasSuffix(tag, "-latest") || !strings.Contains(last, ":"))
	add(mutable, "Image version reference", r.Runtime.Image, "Pin a digest or commit-tagged image for reproducible deployments; keep it updated.", model.Warn)
	for _, mount := range r.Runtime.Mounts {
		if strings.Contains(mount.Source, "docker.sock") || strings.Contains(mount.Destination, "docker.sock") {
			out = append(out, Finding{SiteID: site.ID, State: model.Fail, Title: "Docker socket mounted", Evidence: mount.Destination, Investigation: "Docker socket access grants powerful host control, even with a read-only bind mount."})
		}
	}
	return out
}
func Published(site config.Site, r model.Runtime, caddyName string) []Exposure {
	out := []Exposure{}
	for port, bindings := range r.Ports {
		for _, binding := range bindings {
			state := model.Warn
			evidence := "Potential network exposure; firewall and internet reachability were not tested"
			ip := net.ParseIP(binding.HostIP)
			if ip != nil && ip.IsLoopback() {
				state = model.Pass
				evidence = "Published only on loopback"
			} else if r.Name == "/"+caddyName || r.Name == caddyName {
				if binding.HostPort == "80" || binding.HostPort == "443" {
					state = model.Pass
					evidence = "Expected Caddy HTTP/HTTPS publication"
				}
			} else if strings.HasPrefix(port, "5432/") || strings.HasPrefix(port, "6379/") || strings.HasPrefix(port, "3306/") {
				state = model.Fail
				evidence = "Database port published beyond loopback; external reachability unverified"
			}
			out = append(out, Exposure{SiteID: site.ID, Container: site.Runtime.Container, HostIP: binding.HostIP, HostPort: binding.HostPort, ContainerPort: port, State: state, Evidence: evidence})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].HostPort+out[i].HostIP < out[j].HostPort+out[j].HostIP })
	return out
}
func Inspect(ctx context.Context, c config.Config, reports []model.Report, runner platform.Runner) Report {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r := Report{HostID: c.Host.ID, ObservedAt: time.Now().UTC(), Findings: []Finding{}, Exposures: []Exposure{}, Containers: []Container{}, PossibleOrphans: []Container{}, Listening: []string{}, Unavailable: []string{}}
	runner = &platform.Cache{Runner: runner}
	byID := map[string]model.Report{}
	owners := map[string]string{}
	for _, report := range reports {
		byID[report.SiteID] = report
	}
	// Several routes can use one container. Inspect and count that workload once,
	// retaining every endpoint ID as scope rather than duplicating its warnings.
	groups := [][]config.Site{}
	indices := map[string]int{}
	for _, site := range config.OrderedSites(c.Sites) {
		key := "site:" + site.ID
		if site.Runtime.Type == "docker" && site.Runtime.Container != "" {
			key = "container:" + site.Runtime.Container
		}
		if index, ok := indices[key]; ok {
			groups[index] = append(groups[index], site)
		} else {
			indices[key] = len(groups)
			groups = append(groups, []config.Site{site})
		}
	}
	for _, sites := range groups {
		site := sites[0]
		current := byID[site.ID]
		for _, candidate := range sites {
			if report := byID[candidate.ID]; report.Runtime.PostureObserved && report.ObservedAt.After(current.ObservedAt) {
				current = report
			}
		}
		if site.Runtime.Type == "docker" && (!current.Runtime.PostureObserved || !model.Fresh(current.ObservedAt, time.Now(), model.DefaultMaxAge)) {
			metadata, _ := runtimecheck.InspectDocker(ctx, runner, site)
			current.Runtime, current.ObservedAt = metadata, time.Now().UTC()
		}
		ids := []string{}
		for _, endpoint := range sites {
			ids = append(ids, endpoint.ID)
		}
		for _, finding := range Posture(site, current) {
			finding.Container, finding.SiteIDs = site.Runtime.Container, ids
			r.Findings = append(r.Findings, finding)
		}
		if site.Runtime.Container != "" {
			owners[site.Runtime.Container] = site.ID
		}
		r.Exposures = append(r.Exposures, Published(site, current.Runtime, c.Host.CaddyContainer)...)
	}
	data, err := runner.Run(ctx, "docker", "ps", "--all", "--format", `{"name":{{json .Names}},"id":{{json .ID}},"image":{{json .Image}},"state":{{json .State}},"ports":{{json .Ports}}}`)
	if err != nil {
		r.Unavailable = append(r.Unavailable, "Docker inventory")
	} else {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var container Container
			if json.Unmarshal([]byte(line), &container) != nil {
				continue
			}
			container.SiteID = owners[container.Name]
			r.Containers = append(r.Containers, container)
			if container.SiteID == "" && container.Name != c.Host.CaddyContainer && (container.State == "exited" || container.State == "dead") {
				r.PossibleOrphans = append(r.PossibleOrphans, container)
			}
		}
	}
	for _, container := range r.Containers {
		if container.SiteID != "" {
			continue
		}
		site := config.Site{HostID: c.Host.ID, Runtime: config.Runtime{Type: "docker", Container: container.Name}}
		current := byID["container:"+container.Name]
		metadata := current.Runtime
		if !metadata.PostureObserved || !model.Fresh(current.ObservedAt, time.Now(), model.DefaultMaxAge) {
			metadata, _ = runtimecheck.InspectDocker(ctx, runner, site)
			current = model.Report{ObservedAt: time.Now(), Runtime: metadata}
		}
		if metadata.Status == "" {
			r.Unavailable = append(r.Unavailable, "Published ports unavailable for "+container.Name)
			continue
		}
		r.Exposures = append(r.Exposures, Published(site, metadata, c.Host.CaddyContainer)...)
		if metadata.Status == "running" {
			for _, finding := range Posture(site, current) {
				finding.Container = container.Name
				r.Findings = append(r.Findings, finding)
			}
		}
	}
	data, err = runner.Run(ctx, "ss", "-H", "-lnt")
	if err != nil {
		r.Unavailable = append(r.Unavailable, "Host listening sockets")
	} else {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) >= 4 {
				r.Listening = append(r.Listening, f[3])
			}
		}
	}
	return r
}
func Ownership(c config.Config, resource string) []config.Site {
	out := []config.Site{}
	for _, s := range c.Sites {
		if resource == s.ID || resource == s.Runtime.Container || resource == s.Proxy.Domain || resource == s.Proxy.Upstream || resource == strconv.Itoa(s.Runtime.Port) {
			out = append(out, s)
		}
	}
	return out
}
