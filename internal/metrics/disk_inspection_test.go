package metrics

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
)

func TestDirectoryIndexHandlesUnusualNamesAndDoesNotAcceptTruncatedRecords(t *testing.T) {
	data := []byte("40\t/srv/name\twith\ncontrols\x0050\t/srv/complete\x0060\t/srv/trunc")
	rows, _ := parseDirectories(data, true, nil)
	if len(rows) != 2 || !rows[0].Partial || !rows[1].Partial {
		t.Fatal(rows)
	}
	if !strings.Contains(fmt.Sprint(rows), "controls") || strings.Contains(fmt.Sprint(rows), "trunc") {
		t.Fatal(rows)
	}
}
func TestIndexLimitRetainsStorageOwnershipTargetsAndRoot(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxDiskDirectories+20; i++ {
		fmt.Fprintf(&b, "100\t/a/b/dir%d\x00", i)
	}
	b.WriteString("999\t/\x0077\t/a/deep/important\x00")
	rows, truncated := parseDirectories([]byte(b.String()), false, map[string]bool{"/": true, "/a/deep/important": true})
	found := map[string]bool{}
	for _, r := range rows {
		found[r.Path] = true
	}
	if !truncated || len(rows) != MaxDiskDirectories || !found["/"] || !found["/a/deep/important"] {
		t.Fatal("bounded index lost ownership measurements")
	}
}
func TestStorageMappingDoesNotCountParentsAndChildrenOrInventContainerPaths(t *testing.T) {
	sites := []config.Site{{ID: "a", Proxy: config.Proxy{Domain: "a.example"}}, {ID: "aw", Proxy: config.Proxy{Domain: "www.a.example"}}, {ID: "b", Proxy: config.Proxy{Domain: "b.example"}}, {ID: "c", Proxy: config.Proxy{Domain: "c.example"}}}
	registry := discovery.Registry{Snapshot: discovery.Snapshot{Workloads: []discovery.Workload{{ID: "proxy", Type: "docker", Mounts: []discovery.Mount{{Source: "/srv", Destination: "/web"}}}}}, Sites: []discovery.Site{{ID: "a", Domain: "a.example", Routes: []discovery.Route{{Root: "/web/alpha", ProxyID: "proxy"}}}, {ID: "c", Domain: "c.example", Routes: []discovery.Route{{Root: "/image-only", ProxyID: "proxy"}}}}}
	bindings := diskBindings(registry, sites)
	if len(bindings["a"]) != 1 || bindings["a"][0] != "/srv/alpha" || len(bindings["c"]) != 0 {
		t.Fatal("container root was guessed or not mapped", bindings)
	}
	bindings["a"] = []string{"/srv/alpha", "/srv/alpha/logs"}
	bindings["aw"] = []string{"/srv/alpha"}
	bindings["b"] = []string{"/srv/alpha/logs"}
	rows := siteStorage(sites, registry, bindings, []DiskEntry{{Path: "/srv/alpha", Bytes: 100}, {Path: "/srv/alpha/logs", Bytes: 20}}, nil)
	if len(rows) != 3 || rows[0].DirectoryBytes == nil || *rows[0].DirectoryBytes != 100 || !rows[0].Shared || rows[2].DirectoryBytes != nil {
		t.Fatal("storage double counted, lost sharing or invented missing totals", rows)
	}
	rows = siteStorage(sites, registry, map[string][]string{"a": {"/missing"}}, nil, nil)
	if rows[0].DirectoryBytes != nil || !rows[0].Partial {
		t.Fatal("unmeasured directory became a measured zero")
	}
}
func TestPairedAddressWritableLayersAreDeduplicatedAndMissingSizesStayPartial(t *testing.T) {
	sites := []config.Site{{ID: "a", Proxy: config.Proxy{Domain: "a.example"}}, {ID: "aw", Proxy: config.Proxy{Domain: "www.a.example"}}}
	registry := discovery.Registry{Sites: []discovery.Site{{Domain: "a.example", WorkloadIDs: []string{"shared", "base"}}, {Domain: "www.a.example", WorkloadIDs: []string{"shared", "alias"}}}}
	rows := siteStorage(sites, registry, nil, nil, map[string]int64{"shared": 10, "base": 20, "alias": 30})
	if len(rows) != 1 || rows[0].WritableBytes == nil || *rows[0].WritableBytes != 60 || rows[0].WritablePartial {
		t.Fatal("paired addresses double counted or lost a writable layer", rows)
	}
	rows = siteStorage(sites, registry, nil, nil, map[string]int64{"shared": 0, "base": 20})
	if rows[0].WritableBytes == nil || *rows[0].WritableBytes != 20 || !rows[0].WritablePartial {
		t.Fatal("missing alias size became a complete measurement", rows)
	}
}

func TestGrowthRequiresComparableCompleteDirectoryMeasurements(t *testing.T) {
	before := DiskReport{Directories: []DiskEntry{{Path: "/a", Bytes: 100}, {Path: "/b", Bytes: 100, Partial: true}}}
	after := DiskReport{Directories: []DiskEntry{{Path: "/a", Bytes: 120}, {Path: "/b", Bytes: 50}, {Path: "/c", Bytes: 20}}}
	DiskGrowth(&after, &before)
	if after.Directories[0].GrowthBytes == nil || *after.Directories[0].GrowthBytes != 20 || after.Directories[1].GrowthBytes != nil || after.Directories[2].GrowthBytes != nil {
		t.Fatal(after)
	}
}
