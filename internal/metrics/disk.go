package metrics

import (
	"bufio"
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/platform"
)

type DiskEntry struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}
type DiskReport struct {
	HostID      string              `json:"host_id"`
	ObservedAt  time.Time           `json:"observed_at"`
	Directories []DiskEntry         `json:"directories"`
	Docker      []map[string]string `json:"docker"`
	Unavailable []string            `json:"unavailable"`
}

func Disk(ctx context.Context, runner platform.Runner, h config.Host) DiskReport {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	r := DiskReport{HostID: h.ID, ObservedAt: time.Now().UTC(), Directories: []DiskEntry{}, Docker: []map[string]string{}, Unavailable: []string{}}
	for _, root := range []string{"/home/deploy/apps", "/var/lib/docker", "/var/log", "/tmp"} {
		data, err := runner.Run(ctx, "du", "-x", "-B1", "--max-depth=1", "--", root)
		if err != nil {
			r.Unavailable = append(r.Unavailable, root+": permission denied, unavailable or timed out")
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			size, path, ok := strings.Cut(scanner.Text(), "\t")
			if !ok {
				continue
			}
			bytes, err := strconv.ParseInt(size, 10, 64)
			if err == nil {
				r.Directories = append(r.Directories, DiskEntry{Path: path, Bytes: bytes})
			}
		}
	}
	data, err := runner.Run(ctx, "docker", "system", "df", "--format", "{{json .}}")
	if err != nil {
		r.Unavailable = append(r.Unavailable, "Docker disk accounting")
	} else {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var fields map[string]string
			if json.Unmarshal([]byte(line), &fields) == nil {
				r.Docker = append(r.Docker, fields)
			}
		}
	}
	sort.Slice(r.Directories, func(i, j int) bool { return r.Directories[i].Bytes > r.Directories[j].Bytes })
	return r
}
