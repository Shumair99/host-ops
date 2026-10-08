package collector

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
)

type proxyProcess struct {
	kind, binary string
	args         []string
	workload     discovery.Workload
}

func netAddress(host, port string) string { return net.JoinHostPort(host, port) }
func processUnit(pid int) string {
	data, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cgroup"))
	for _, line := range strings.Split(string(data), "\n") {
		for _, part := range strings.Split(line, "/") {
			if strings.HasSuffix(part, ".service") {
				return part
			}
		}
	}
	return ""
}

func readProcesses(ctx context.Context, runner platform.Runner, snapshot *discovery.Snapshot) []proxyProcess {
	source := discovery.Source{ID: "linux/processes", Kind: "processes", State: "ready", Mode: "active", ObservedAt: snapshot.ObservedAt}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		source.State, source.Issue = "unavailable", "Process inventory unreadable"
		snapshot.Sources = append(snapshot.Sources, source)
		return nil
	}
	result := []proxyProcess{}
	byPID := map[int]discovery.Workload{}
	containerPIDs := map[int]bool{}
	existing := map[string]bool{}
	for _, w := range snapshot.Workloads {
		containerPIDs[w.PID] = true
		existing[w.ID] = true
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || containerPIDs[pid] {
			continue
		}
		// Container descendants belong to the runtime reader's network namespace.
		cgroup, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cgroup"))
		if strings.Contains(string(cgroup), "docker-") || strings.Contains(string(cgroup), "/docker/") || strings.Contains(string(cgroup), "libpod-") {
			continue
		}
		binary, err := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		if err != nil {
			continue
		}
		unit := processUnit(pid)
		name := filepath.Base(binary)
		kind := proxyKind(binary, "")
		typ := "process"
		identity := strconv.Itoa(pid)
		if unit != "" {
			typ, name, identity = "systemd", unit, unit
		}
		w := discovery.Workload{ID: discovery.StableID("workload", snapshot.HostID+"|"+typ+"|"+identity), SourceID: source.ID, Type: typ, Name: name, Status: "running", PID: pid, ProxyKind: kind}
		byPID[pid] = w
		if kind != "" {
			cmd, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
			args := strings.Split(strings.TrimRight(string(cmd), "\x00"), "\x00")
			result = append(result, proxyProcess{kind: kind, binary: binary, args: args, workload: w})
		}
	}
	seen := existing
	data, err := runner.Run(ctx, "ss", "-H", "-lntup")
	if err != nil {
		snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "linux/listeners", ParentID: source.ID, Kind: "listeners", State: "unavailable", Mode: "active", Issue: "Listening socket inspection unavailable", ObservedAt: snapshot.ObservedAt})
	} else {
		if unixData, unixErr := runner.Run(ctx, "ss", "-H", "-lxnp"); unixErr == nil {
			data = append(append(data, '\n'), unixData...)
		} else {
			snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "linux/unix-listeners", ParentID: source.ID, Kind: "listeners", State: "unavailable", Mode: "active", Issue: "Unix socket ownership unavailable", ObservedAt: snapshot.ObservedAt})
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			match := regexp.MustCompile(`pid=(\d+)`).FindStringSubmatch(line)
			pid := 0
			if len(match) > 1 {
				pid, _ = strconv.Atoi(match[1])
			}
			unit := processUnit(pid)
			snapshot.Listeners = append(snapshot.Listeners, discovery.Listener{Address: fields[4], PID: pid, Unit: unit})
			if w, ok := byPID[pid]; ok && !seen[w.ID] {
				snapshot.Workloads = append(snapshot.Workloads, w)
				seen[w.ID] = true
			}
		}
		snapshot.Sources = append(snapshot.Sources, discovery.Source{ID: "linux/listeners", ParentID: source.ID, Kind: "listeners", State: "ready", Mode: "active", ObservedAt: snapshot.ObservedAt})
	}
	for _, proxy := range result {
		if !seen[proxy.workload.ID] {
			snapshot.Workloads = append(snapshot.Workloads, proxy.workload)
			seen[proxy.workload.ID] = true
		}
	}
	snapshot.Sources = append(snapshot.Sources, source)
	return result
}
