package collector

import (
	"context"
	"encoding/json"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
)

type containerMeta struct {
	ID, Name, Path, Image string
	Args                  []string
	Config                struct {
		Image           string
		Labels          map[string]string
		Cmd, Entrypoint []string
	}
	State struct {
		Status      string
		Health      *struct{ Status string }
		Healthcheck *struct{ Status string }
		Pid         int
	}
	HostConfig struct {
		RestartPolicy struct{ Name string }
		NetworkMode   string
	}
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string
			Aliases   []string
		}
		Ports map[string][]struct{ HostIP, HostPort string }
	}
	Mounts []struct{ Source, Destination string }
	Engine string
}

const discoveryInspect = `{"Id":{{json .Id}},"Name":{{json .Name}},"Path":{{json .Path}},"Args":{{json .Args}},"Config":{"Image":{{json .Config.Image}},"Labels":{"com.docker.compose.project":{{json (index .Config.Labels "com.docker.compose.project")}},"com.docker.compose.service":{{json (index .Config.Labels "com.docker.compose.service")}},"org.opencontainers.image.revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}}},"State":{"Status":{{json .State.Status}},"Pid":{{.State.Pid}},"Health":{{if .State.Health}}{"Status":{{json .State.Health.Status}}}{{else}}null{{end}}},"HostConfig":{"RestartPolicy":{"Name":{{json .HostConfig.RestartPolicy.Name}}},"NetworkMode":{{json .HostConfig.NetworkMode}}},"NetworkSettings":{"Networks":{{json .NetworkSettings.Networks}},"Ports":{{json .NetworkSettings.Ports}}},"Mounts":{{json .Mounts}}}`

func readRuntime(ctx context.Context, runner platform.Runner, engine string, snapshot *discovery.Snapshot) []containerMeta {
	source := discovery.Source{ID: "runtime/" + engine, Kind: engine, Mode: "active", State: "ready", ObservedAt: snapshot.ObservedAt}
	if _, err := exec.LookPath(engine); err != nil {
		source.State = "absent"
		snapshot.Sources = append(snapshot.Sources, source)
		return nil
	}
	data, err := runner.Run(ctx, engine, "ps", "--all", "--quiet", "--no-trunc")
	if err != nil {
		source.State, source.Issue = "unavailable", "Runtime inventory access failed"
		snapshot.Sources = append(snapshot.Sources, source)
		return nil
	}
	ids := strings.Fields(string(data))
	out := []containerMeta{}
	for start := 0; start < len(ids); start += 32 {
		end := min(len(ids), start+32)
		const selectedLabels = `{"com.docker.compose.project":{{json (index .Config.Labels "com.docker.compose.project")}},"com.docker.compose.service":{{json (index .Config.Labels "com.docker.compose.service")}},"org.opencontainers.image.revision":{{json (index .Config.Labels "org.opencontainers.image.revision")}}}`
		// Only selected identity/routing facts leave the collector. Arbitrary
		// labels and authentication middleware values are discarded locally.
		format := strings.Replace(discoveryInspect, selectedLabels, `{{json .Config.Labels}}`, 1)
		args := []string{"container", "inspect", "--format", format, "--"}
		if engine == "podman" {
			args = []string{"container", "inspect", "--format", "{{json .}}", "--"}
		}
		data, err = runner.Run(ctx, engine, append(args, ids[start:end]...)...)
		if err != nil {
			source.State, source.Issue = "unavailable", "Container metadata read incomplete"
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var meta containerMeta
			if json.Unmarshal([]byte(line), &meta) != nil {
				source.State, source.Issue = "unavailable", "Container metadata could not be decoded"
				continue
			}
			meta.Engine = engine
			meta.Name = strings.TrimPrefix(meta.Name, "/")
			out = append(out, meta)
			project, service := meta.Config.Labels["com.docker.compose.project"], meta.Config.Labels["com.docker.compose.service"]
			identity := meta.Name
			if project != "" && service != "" {
				identity = project + "/" + service
			}
			// Replicas share a service; retain their individual instances in the graph.
			w := discovery.Workload{ID: discovery.StableID("workload", snapshot.HostID+"|"+engine+"|"+identity+"|"+meta.Name), InstanceID: meta.ID, SourceID: source.ID, Type: engine, Name: meta.Name, Project: project, Service: service, Image: meta.Config.Image, Revision: meta.Config.Labels["org.opencontainers.image.revision"], Status: meta.State.Status, Health: "none", RestartPolicy: meta.HostConfig.RestartPolicy.Name, Networks: map[string]discovery.Network{}, Ports: map[string][]string{}, PID: meta.State.Pid, ProxyKind: proxyKind(meta.Path, meta.Config.Image)}
			if meta.State.Health != nil {
				w.Health = meta.State.Health.Status
			}
			if meta.State.Healthcheck != nil {
				w.Health = meta.State.Healthcheck.Status
			}
			for name, details := range meta.NetworkSettings.Networks {
				w.Networks[engine+":"+name] = discovery.Network{IP: details.IPAddress, Aliases: details.Aliases}
			}
			for name, bindings := range meta.NetworkSettings.Ports {
				w.Ports[name] = []string{}
				for _, binding := range bindings {
					ip := binding.HostIP
					if ip == "" {
						ip = "0.0.0.0"
					}
					w.Ports[name] = append(w.Ports[name], netAddress(ip, binding.HostPort))
				}
			}
			for _, mount := range meta.Mounts {
				w.Mounts = append(w.Mounts, discovery.Mount{Source: mount.Source, Destination: mount.Destination})
			}
			snapshot.Workloads = append(snapshot.Workloads, w)
		}
	}
	snapshot.Sources = append(snapshot.Sources, source)
	return out
}

func proxyKind(executable, image string) string {
	name := path.Base(executable)
	if name == "docker-entrypoint.sh" || name == "" || name == "." {
		name = path.Base(strings.Split(strings.Split(image, "@")[0], ":")[0])
	}
	switch name {
	case "caddy", "nginx", "traefik":
		return name
	case "httpd", "apache2", "apache2ctl", "apachectl":
		return "apache"
	}
	return ""
}

func inspectionContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 25*time.Second)
}
