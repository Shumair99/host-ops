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
		return []Finding{{SiteID: site.ID, State: model.Unknown, Title: "Container posture does not apply", Evidence: "Runtime: " + site.Runtime.Type, Investigation: "Assess permissions and exposure for this runtime separately."}}
	}
	if r.Runtime.Status == "" || !model.Fresh(r.ObservedAt, time.Now(), model.DefaultMaxAge) {
		return []Finding{{SiteID: site.ID, State: model.Unknown, Title: "Fresh runtime metadata unavailable", Investigation: "Collect runtime state before evaluating security posture."}}
	}
	out := []Finding{}
	add := func(condition bool, title, evidence, investigation string, severity model.State) {
		state := model.Pass
		if condition {
			state = severity
		}
		out = append(out, Finding{SiteID: site.ID, State: state, Title: title, Evidence: evidence, Investigation: investigation})
	}
	user := strings.Split(r.Runtime.User, ":")[0]
	add(user == "" || user == "root" || user == "0", "Configured runtime user", "Docker user: "+r.Runtime.User, "Review running as an unprivileged user.", model.Warn)
	add(r.Runtime.Privileged, "Privileged execution", "Docker privileged flag", "Remove unnecessary privileged execution after verifying dependencies.", model.Fail)
	add(r.Runtime.MemoryLimit == 0, "Memory limit", "Configured bytes: "+strconv.FormatInt(r.Runtime.MemoryLimit, 10), "Configure a suitable memory limit through Compose.", model.Warn)
	add(!r.Runtime.ReadOnly, "Writable root filesystem", "Docker read-only root flag", "Review whether a read-only root with explicit writable mounts is practical.", model.Warn)
	last := r.Runtime.Image[strings.LastIndex(r.Runtime.Image, "/")+1:]
	add(strings.HasSuffix(last, ":latest") || !strings.Contains(last, ":") && !strings.Contains(last, "@"), "Image version reference", r.Runtime.Image, "Prefer an immutable digest or commit-tagged image.", model.Warn)
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
	for _, site := range c.Sites {
		owners[site.Runtime.Container] = site.ID
		r.Findings = append(r.Findings, Posture(site, byID[site.ID])...)
		r.Exposures = append(r.Exposures, Published(site, byID[site.ID].Runtime, c.Host.CaddyContainer)...)
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
		metadata, _ := runtimecheck.InspectDocker(ctx, runner, site)
		if metadata.Status == "" {
			r.Unavailable = append(r.Unavailable, "Published ports unavailable for "+container.Name)
			continue
		}
		r.Exposures = append(r.Exposures, Published(site, metadata, c.Host.CaddyContainer)...)
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
