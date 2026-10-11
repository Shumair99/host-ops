package metrics

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
)

const MaxDiskDirectories = 4096

func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
func NormalFilesystem(kind string) bool {
	switch kind {
	case "tmpfs", "devtmpfs", "overlay", "aufs", "proc", "sysfs", "cgroup", "cgroup2", "squashfs", "nsfs", "fusectl", "securityfs", "debugfs", "tracefs", "efivarfs", "autofs", "hugetlbfs", "pstore", "rpc_pipefs", "mqueue", "configfs", "binfmt_misc", "bpf", "nfs", "nfs4", "cifs", "smb3":
		return false
	}
	return kind != ""
}

func parseFilesystems(data []byte, inodes bool) []Filesystem {
	out := []Filesystem{}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		total, e := strconv.ParseInt(f[2], 10, 64)
		if e != nil {
			continue
		}
		used, _ := strconv.ParseInt(f[3], 10, 64)
		free, _ := strconv.ParseInt(f[4], 10, 64)
		mount := strings.Join(f[6:], " ")
		s := Filesystem{Mount: mount, Type: f[1]}
		if inodes {
			s.Inodes, s.InodesUsed = total, used
		} else {
			s.TotalBytes, s.UsedBytes, s.AvailableBytes = total, used, free
		}
		out = append(out, s)
	}
	return out
}

func diskBindings(inventory discovery.Registry, sites []config.Site) map[string][]string {
	bindings := map[string][]string{}
	workloads := map[string]discovery.Workload{}
	for _, w := range inventory.Snapshot.Workloads {
		workloads[w.ID] = w
	}
	byDomain := map[string]discovery.Site{}
	for _, s := range inventory.Sites {
		byDomain[s.Domain] = s
	}
	add := func(id, p string) {
		p = path.Clean(p)
		if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\r\n") || within(p, "/proc") || within(p, "/sys") || within(p, "/dev") {
			return
		}
		for _, old := range bindings[id] {
			if old == p {
				return
			}
		}
		bindings[id] = append(bindings[id], p)
	}
	for _, s := range sites {
		item := byDomain[s.Proxy.Domain]
		for _, id := range item.WorkloadIDs {
			for _, mount := range workloads[id].Mounts {
				add(s.ID, mount.Source)
			}
		}
		if s.Runtime.Directory != "" {
			add(s.ID, s.Runtime.Directory)
		}
		for _, route := range item.Routes {
			if route.Root == "" {
				continue
			}
			proxy, ok := workloads[route.ProxyID]
			if !ok || (proxy.Type != "docker" && proxy.Type != "podman") {
				add(s.ID, route.Root)
				continue
			}
			// Container document roots are mapped only through observed host mounts.
			best := ""
			var chosen discovery.Mount
			for _, mount := range proxy.Mounts {
				if within(route.Root, mount.Destination) && len(mount.Destination) > len(best) {
					best = mount.Destination
					chosen = mount
				}
			}
			if best != "" {
				add(s.ID, path.Join(chosen.Source, strings.TrimPrefix(strings.TrimPrefix(route.Root, best), "/")))
			}
		}
	}
	return bindings
}

func minimalPaths(paths []string) []string {
	paths = append([]string(nil), paths...)
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) < len(paths[j]) })
	out := []string{}
	for _, p := range paths {
		covered := false
		for _, root := range out {
			covered = covered || within(p, root)
		}
		if !covered {
			out = append(out, p)
		}
	}
	return out
}

func parseDirectories(data []byte, partial bool, important map[string]bool) ([]DiskEntry, bool) {
	out := []DiskEntry{}
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		parts = parts[:len(parts)-1]
	}
	for _, line := range parts {
		at := strings.IndexByte(line, '\t')
		if at < 0 {
			continue
		}
		bytes, err := strconv.ParseInt(line[:at], 10, 64)
		p := line[at+1:]
		if err != nil || bytes < 0 || !strings.HasPrefix(p, "/") {
			continue
		}
		parent := path.Dir(p)
		if p == "/" {
			parent = ""
		}
		out = append(out, DiskEntry{Path: p, Parent: parent, Bytes: bytes, Partial: partial})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if important[a.Path] != important[b.Path] {
			return important[a.Path]
		}
		da, db := strings.Count(a.Path, "/"), strings.Count(b.Path, "/")
		if da != db {
			return da < db
		}
		return a.Bytes > b.Bytes
	})
	truncated := len(out) > MaxDiskDirectories
	if truncated {
		out = out[:MaxDiskDirectories]
	}
	return out, truncated
}

func inspectDisk(parent context.Context, runner platform.Runner, h config.Host, inventory discovery.Registry, sites []config.Site) DiskReport {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	start := time.Now().UTC()
	r := DiskReport{HostID: h.ID, ObservedAt: start, Directories: []DiskEntry{}, Docker: []map[string]string{}, Unavailable: []string{}}
	read := func(seconds int, name string, args ...string) ([]byte, error) {
		c, done := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer done()
		return runner.Run(c, name, args...)
	}
	data, err := read(5, "df", "-P", "-T", "-B1")
	if err != nil {
		r.Unavailable = append(r.Unavailable, "Filesystem capacity")
	} else {
		r.Filesystems = parseFilesystems(data, false)
	}
	data, err = read(5, "df", "-P", "-T", "-i")
	if err == nil {
		for _, in := range parseFilesystems(data, true) {
			for i := range r.Filesystems {
				if r.Filesystems[i].Mount == in.Mount {
					r.Filesystems[i].Inodes, r.Filesystems[i].InodesUsed = in.Inodes, in.InodesUsed
				}
			}
		}
	} else {
		r.Unavailable = append(r.Unavailable, "Filesystem inode usage")
	}
	roots := append([]string(nil), h.DiskRoots...)
	if len(roots) == 0 {
		roots = []string{"/"}
		for _, fs := range r.Filesystems {
			if fs.Mount != "/" && NormalFilesystem(fs.Type) {
				roots = append(roots, fs.Mount)
			}
		}
	}
	// Each mount is traversed once with -x. Parent/child roots on that same
	// filesystem are redundant, while a mounted filesystem needs its own root.
	unique := []string{}
	sort.SliceStable(roots, func(i, j int) bool { return len(roots[i]) < len(roots[j]) })
	for _, root := range roots {
		root = path.Clean(root)
		covered := false
		for _, old := range unique {
			if root == old {
				covered = true
			}
			if within(root, old) {
				separate := false
				for _, fs := range r.Filesystems {
					separate = separate || fs.Mount == root
				}
				covered = covered || !separate
			}
		}
		if !covered {
			unique = append(unique, root)
		}
	}
	r.Roots = unique
	bindings := diskBindings(inventory, sites)
	important := map[string]bool{}
	for _, paths := range bindings {
		for _, p := range paths {
			important[p] = true
		}
	}
	for _, root := range unique {
		important[root] = true
	}
	args := []string{"45s", "nice", "-n", "15", "du", "-x", "-0", "-B1", "--max-depth=8", "--"}
	args = append(args, unique...)
	data, err = read(48, "timeout", args...)
	partial := err != nil
	if partial {
		r.Unavailable = append(r.Unavailable, "Directory accounting incomplete: permissions, timeout or output limit")
	}
	var truncated bool
	r.Directories, truncated = parseDirectories(data, partial, important)
	if truncated {
		r.Unavailable = append(r.Unavailable, "Directory index limited to 4096 entries; deeper paths remain in ancestor totals")
	}
	data, err = read(8, "docker", "system", "df", "--format", "{{json .}}")
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
	writable := map[string]int64{}
	names, ids := []string{}, map[string]string{}
	for _, w := range inventory.Snapshot.Workloads {
		if w.Type == "docker" && len(names) < 100 {
			names = append(names, w.Name)
			ids[w.Name] = w.ID
		}
	}
	if len(names) > 0 {
		args := []string{"container", "inspect", "--size", "--format", `{"name":{{json .Name}},"writable":{{json .SizeRw}}}`}
		args = append(args, names...)
		data, err = read(8, "docker", args...)
		if err != nil {
			r.Unavailable = append(r.Unavailable, "Container writable-layer sizes")
		} else {
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var v struct {
					Name     string
					Writable *int64
				}
				if json.Unmarshal([]byte(line), &v) == nil && v.Writable != nil {
					if id := ids[strings.TrimPrefix(v.Name, "/")]; id != "" {
						writable[id] = *v.Writable
					}
				}
			}
		}
	}
	r.Sites = siteStorage(sites, inventory, bindings, r.Directories, writable)
	if len(writable) > 0 {
		// Writable layers belong to the daemon's actual storage filesystem;
		// never assume Docker's data directory is on the host's root disk.
		if data, err = read(5, "docker", "info", "--format", "{{json .DockerRootDir}}"); err == nil {
			var root string
			if json.Unmarshal(data, &root) == nil && strings.HasPrefix(root, "/") {
				mount := StorageFilesystem(root, r.Filesystems)
				for i := range r.Sites {
					if r.Sites[i].WritableBytes != nil {
						r.Sites[i].WritableFilesystem = mount
					}
				}
			}
		}
	}
	r.CompletedAt = time.Now().UTC()
	r.DurationSeconds = time.Since(start).Seconds()
	return r
}

// StorageFilesystem uses the longest observed mount, including non-physical
// filesystems, so a tmpfs/bind/secondary mount cannot be priced as root storage.
func StorageFilesystem(p string, filesystems []Filesystem) string {
	if !strings.HasPrefix(p, "/") {
		return ""
	}
	best := ""
	for _, fs := range filesystems {
		if within(path.Clean(p), fs.Mount) && len(fs.Mount) > len(best) {
			best = fs.Mount
		}
	}
	return best
}

func siteStorage(sites []config.Site, inventory discovery.Registry, bindings map[string][]string, directories []DiskEntry, writable map[string]int64) []SiteStorage {
	byPath := map[string]DiskEntry{}
	for _, e := range directories {
		byPath[e.Path] = e
	}
	byDomain := map[string]discovery.Site{}
	for _, s := range inventory.Sites {
		byDomain[s.Domain] = s
	}
	base := map[string]string{}
	for _, s := range sites {
		base[s.Proxy.Domain] = s.ID
	}
	out := []SiteStorage{}
	for _, s := range sites {
		if strings.HasPrefix(s.Proxy.Domain, "www.") && base[strings.TrimPrefix(s.Proxy.Domain, "www.")] != "" {
			continue
		}
		paths := append([]string(nil), bindings[s.ID]...)
		alias := base["www."+s.Proxy.Domain]
		if alias != "" {
			paths = append(paths, bindings[alias]...)
		}
		paths = minimalPaths(paths)
		item := SiteStorage{SiteID: s.ID, Name: s.Proxy.Domain, Paths: paths}
		if item.Name == "" {
			item.Name = s.Name
		}
		if len(paths) > 0 {
			total := int64(0)
			known := false
			for _, p := range paths {
				if e, ok := byPath[p]; ok {
					known = true
					total += e.Bytes
					item.Partial = item.Partial || e.Partial
				} else {
					item.Partial = true
				}
				for other, ps := range bindings {
					if other == s.ID || other == alias {
						continue
					}
					for _, op := range ps {
						if within(op, p) || within(p, op) {
							item.Shared = true
						}
					}
				}
			}
			if known {
				item.DirectoryBytes = &total
			}
		}
		if writable != nil {
			total := int64(0)
			known := false
			workloadIDs := append([]string(nil), byDomain[s.Proxy.Domain].WorkloadIDs...)
			if alias != "" {
				workloadIDs = append(workloadIDs, byDomain["www."+s.Proxy.Domain].WorkloadIDs...)
			}
			seen := map[string]bool{}
			for _, id := range workloadIDs {
				if seen[id] {
					continue
				}
				seen[id] = true
				for domain, other := range byDomain {
					if domain == s.Proxy.Domain || domain == "www."+s.Proxy.Domain {
						continue
					}
					for _, oid := range other.WorkloadIDs {
						if oid == id {
							item.Shared = true
						}
					}
				}
				if size, ok := writable[id]; ok {
					total += size
					known = true
				} else {
					item.WritablePartial = true
				}
			}
			if known {
				item.WritableBytes = &total
			}
		}
		out = append(out, item)
	}
	return out
}

func DiskGrowth(current *DiskReport, previous *DiskReport) {
	if previous == nil {
		return
	}
	old := map[string]DiskEntry{}
	for _, e := range previous.Directories {
		old[e.Path] = e
	}
	for i, e := range current.Directories {
		if before, ok := old[e.Path]; ok && !before.Partial && !e.Partial {
			delta := e.Bytes - before.Bytes
			current.Directories[i].GrowthBytes = &delta
		}
	}
}
