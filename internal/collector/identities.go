package collector

import (
	"context"
	"fmt"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/platform"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func processIdentities(ctx context.Context, runner platform.Runner, engine, name string, mainPID int) model.ProcessSnapshot {
	data, err := runner.Run(ctx, engine, "top", name, "-eo", "pid,ppid,euid,comm")
	if err != nil {
		return model.ProcessSnapshot{Issue: "Process identities unavailable"}
	}
	return readIdentities("/proc", mainPID, string(data))
}

func readIdentities(proc string, mainPID int, data string) model.ProcessSnapshot {
	result := model.ProcessSnapshot{Observed: true, Complete: true}
	mapping, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(mainPID), "uid_map"))
	if err != nil {
		return model.ProcessSnapshot{Issue: "Container UID mapping unavailable"}
	}
	type uidRange struct{ inside, outside, length int }
	ranges := []uidRange{}
	for _, line := range strings.Split(string(mapping), "\n") {
		var r uidRange
		if _, err := fmt.Sscan(line, &r.inside, &r.outside, &r.length); err == nil {
			ranges = append(ranges, r)
		}
	}
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) < 2 {
		return model.ProcessSnapshot{Issue: "Process list empty"}
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 || len(result.Items) >= 256 {
			result.Complete = false
			continue
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		uid, e3 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil || e3 != nil {
			result.Complete = false
			continue
		}
		mapped := false
		for _, r := range ranges {
			if uid >= r.outside && uid-r.outside < r.length {
				uid = r.inside + uid - r.outside
				mapped = true
				break
			}
		}
		if !mapped {
			result.Complete = false
			continue
		}
		p := model.ProcessIdentity{PID: pid, ParentPID: parent, UID: uid, Name: fields[3]}
		if p.Name == "nginx" {
			cmd, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "cmdline"))
			if err != nil {
				result.Complete = false
			} else {
				title := strings.ReplaceAll(string(cmd), "\x00", " ")
				if strings.HasPrefix(title, "nginx: master process ") {
					p.Role = "nginx-master"
				}
				if strings.HasPrefix(title, "nginx: worker process") {
					p.Role = "nginx-worker"
				}
			}
		}
		result.Items = append(result.Items, p)
	}
	if len(result.Items) == 0 {
		result.Complete = false
	}
	if !result.Complete {
		result.Issue = "Process inventory incomplete; no privilege conclusion"
	}
	return result
}
