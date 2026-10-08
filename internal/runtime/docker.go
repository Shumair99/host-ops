package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
)

// Deliberately project metadata; never request Env, health log output or full inspect JSON.
const inspectFormat = `{"name":{{json .Name}},"status":{{json .State.Status}},"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}"none"{{end}},"image":{{json .Config.Image}},"image_id":{{json .Image}},"commit":{{json (index .Config.Labels "org.opencontainers.image.revision")}},"restart_policy":{{json .HostConfig.RestartPolicy.Name}},"restart_count":{{.RestartCount}},"exit_code":{{.State.ExitCode}},"started_at":{{json .State.StartedAt}},"ips":{ {{$first := true}}{{range $name, $net := .NetworkSettings.Networks}}{{if not $first}},{{end}}{{$first = false}}{{json $name}}:{{json $net.IPAddress}}{{end}} },"ports":{{json .NetworkSettings.Ports}},"user":{{json .Config.User}},"privileged":{{.HostConfig.Privileged}},"read_only":{{.HostConfig.ReadonlyRootfs}},"memory_limit":{{.HostConfig.Memory}},"mounts":{{json .Mounts}}}`

func InspectDocker(ctx context.Context, runner platform.Runner, s config.Site) (model.Runtime, []model.Check) {
	r := model.Runtime{HostID: s.HostID, SiteID: s.ID, Type: "docker", Name: s.Runtime.Container}
	check := func(layer string, state model.State, summary string) model.Check {
		return model.Check{HostID: s.HostID, SiteID: s.ID, Layer: layer, State: state, Summary: summary}
	}
	if _, err := runner.Run(ctx, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
		return r, []model.Check{check("docker", model.Unknown, "Docker is unavailable or access is denied"), check("runtime", model.Unknown, "Container could not be inspected")}
	}
	checks := []model.Check{check("docker", model.Pass, "Docker daemon reachable")}
	data, err := runner.Run(ctx, "docker", "container", "inspect", "--format", inspectFormat, "--", s.Runtime.Container)
	if err != nil {
		return r, append(checks, check("runtime", model.Unknown, "Container missing or inspection denied"))
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return r, append(checks, check("runtime", model.Unknown, "Container metadata could not be decoded"))
	}
	r.HostID, r.SiteID, r.Type = s.HostID, s.ID, "docker"
	state := model.Pass
	if r.Status != "running" {
		state = model.Fail
	}
	checks = append(checks, check("runtime", state, fmt.Sprintf("%s; exit code %d; restarts %d", r.Status, r.ExitCode, r.RestartCount)))
	healthState := model.Unknown
	switch r.Health {
	case "healthy":
		healthState = model.Pass
	case "unhealthy":
		healthState = model.Fail
	case "starting":
		healthState = model.Warn
	case "none":
		healthState = model.Warn
	}
	checks = append(checks, check("container_health", healthState, r.Health))
	return r, checks
}
