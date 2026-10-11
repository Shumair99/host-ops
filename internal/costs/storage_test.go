package costs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
	"github.com/Shumair99/host-ops/internal/storage"
)

func diskFixture() (config.Config, map[string][]metrics.Sample, []metrics.Sample, *metrics.DiskReport) {
	c, samples, hosts := lowUsageFixture()
	c.Host.CostWeights = config.CostWeights{}
	directory, writable := int64(9e9), int64(1e9)
	disk := &metrics.DiskReport{HostID: "h", ObservedAt: hosts[0].ObservedAt, CompletedAt: hosts[0].ObservedAt,
		Filesystems: []metrics.Filesystem{{Mount: "/", Type: "ext4", TotalBytes: 40e9, UsedBytes: 35.2e9}},
		Sites:       []metrics.SiteStorage{{SiteID: "a", Paths: []string{"/srv/a"}, DirectoryBytes: &directory, WritableBytes: &writable, WritableFilesystem: "/"}}}
	return c, samples, hosts, disk
}

func TestDefaultAllocationPricesDiskAgainstEntireRootCapacity(t *testing.T) {
	c, samples, hosts, disk := diskFixture()
	r := Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].StorageShare, .25)
	closeTo(t, r.HostStorageUsage, .88)
	closeTo(t, r.Sites[0].Cost, 100*(.1+.2+.25)/3)
	closeTo(t, r.UnusedCost, 100*(1-(.2+.3+.88)/3))
	closeTo(t, number(*r.SiteCost+*r.OtherCost+*r.UnusedCost), 100)
	if r.Sites[0].StorageBytes == nil || *r.Sites[0].StorageBytes != 10e9 || r.Sites[0].UsagePartial || r.Weights.Storage <= 0 {
		t.Fatal("disk evidence lost", r)
	}
	// A site's estimate cannot shrink merely because another measured site exists.
	b := c.Sites[0]
	b.ID, b.Runtime.Container = "b", "b"
	c.Sites = append(c.Sites, b)
	samples["b"] = samples["a"]
	small := int64(1e9)
	disk.Sites = append(disk.Sites, metrics.SiteStorage{SiteID: "b", Paths: []string{"/srv/b"}, DirectoryBytes: &small, WritableBytes: new(int64), WritableFilesystem: "/"})
	r = Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].Cost, 100*(.1+.2+.25)/3)
}

func TestMissingStaleForeignAndSharedDiskRemainPartialNotZero(t *testing.T) {
	cases := map[string]func(*metrics.DiskReport) *metrics.DiskReport{
		"missing": func(_ *metrics.DiskReport) *metrics.DiskReport { return nil },
		"stale": func(d *metrics.DiskReport) *metrics.DiskReport {
			d.ObservedAt = time.Now().Add(-36 * time.Minute)
			return d
		},
		"foreign host": func(d *metrics.DiskReport) *metrics.DiskReport { d.HostID = "other"; return d },
		"future": func(d *metrics.DiskReport) *metrics.DiskReport {
			d.ObservedAt = time.Now().Add(2 * time.Minute)
			return d
		},
		"shared":        func(d *metrics.DiskReport) *metrics.DiskReport { d.Sites[0].Shared = true; return d },
		"invalid bytes": func(d *metrics.DiskReport) *metrics.DiskReport { *d.Sites[0].DirectoryBytes = 41e9; return d },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, samples, hosts, disk := diskFixture()
			c.Sites[0].Business.MonthlyFee = 30
			r := Allocate(c, samples, hosts, mutate(disk))
			a := r.Sites[0]
			closeTo(t, a.Cost, 10)
			if !a.UsagePartial || a.StorageShare != nil || a.Margin != nil || !r.SiteCostPartial {
				t.Fatal("missing disk became complete/free storage", a)
			}
		})
	}
}

func TestDiskFootprintsExcludeOtherMountsAndUnknownWritableScope(t *testing.T) {
	c, samples, hosts, disk := diskFixture()
	disk.Filesystems = append(disk.Filesystems, metrics.Filesystem{Mount: "/srv", Type: "xfs", TotalBytes: 200e9, UsedBytes: 50e9})
	r := Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].StorageShare, .025) // Only writable layer on root.
	disk.Sites[0].WritableFilesystem = "/srv"
	r = Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].StorageShare, 0)
	disk.Sites[0].WritableFilesystem = ""
	r = Allocate(c, samples, hosts, disk)
	if !r.Sites[0].UsagePartial {
		t.Fatal("unknown Docker data root was assumed on root", r)
	}
	disk.Filesystems[1].Type = "tmpfs"
	r = Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].StorageShare, 0)
}

func TestIncompleteDirectoryAccountingKeepsLowerBoundAndNoMargin(t *testing.T) {
	c, samples, hosts, disk := diskFixture()
	disk.Sites[0].Partial = true
	c.Sites[0].Business.MonthlyFee = 40
	r := Allocate(c, samples, hosts, disk)
	closeTo(t, r.Sites[0].StorageShare, .25)
	if !r.Sites[0].UsagePartial || r.Sites[0].Margin != nil || !strings.Contains(r.Sites[0].StorageNote, "lower bound") {
		t.Fatal(r)
	}
}

func TestOverlappingPathsAndImpossibleDiskTotalsAreNeverDoubleCharged(t *testing.T) {
	for _, overlap := range []bool{true, false} {
		c, samples, hosts, disk := diskFixture()
		b := c.Sites[0]
		b.ID, b.Runtime.Container = "b", "b"
		c.Sites = append(c.Sites, b)
		samples["b"] = samples["a"]
		item := disk.Sites[0]
		item.SiteID = "b"
		item.Paths = []string{"/srv/b"}
		if overlap {
			item.Paths = []string{"/srv/a/cache"}
		} else {
			disk.Filesystems[0].UsedBytes = 15e9
		}
		disk.Sites = append(disk.Sites, item)
		r := Allocate(c, samples, hosts, disk)
		for _, a := range r.Sites {
			if a.StorageShare != nil || !a.UsagePartial {
				t.Fatal("duplicate storage was charged", r)
			}
		}
	}
}

func TestSharedRuntimeDiskCountedOnceAndDiskOnlyWeightSupported(t *testing.T) {
	c, samples, _, disk := diskFixture()
	alias := c.Sites[0]
	alias.ID = "alias"
	c.Sites = append(c.Sites, alias)
	samples["alias"] = samples["a"]
	item := disk.Sites[0]
	item.SiteID = "alias"
	disk.Sites = append(disk.Sites, item)
	c.Host.CostWeights = config.CostWeights{Storage: 1}
	r := Allocate(c, nil, nil, disk)
	if len(r.Sites) != 1 || len(r.Sites[0].SiteIDs) != 2 {
		t.Fatal(r)
	}
	closeTo(t, r.SiteCost, 25)
	closeTo(t, r.UnusedCost, 12)
	closeTo(t, r.OtherCost, 63)
}

func TestAssessLoadsRetainedDiskWithoutLaunchingAnotherScan(t *testing.T) {
	c, samples, hosts, disk := diskFixture()
	db, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, sample := range hosts {
		if _, err = db.Add(ctx, "h", "", "host_metrics", sample.ObservedAt, sample); err != nil {
			t.Fatal(err)
		}
	}
	for _, sample := range samples["a"] {
		if _, err = db.Add(ctx, "h", "a", "metrics", sample.ObservedAt, sample); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Transition(ctx, "h", "", "disk_inspection", disk, "", disk.ObservedAt, nil); err != nil {
		t.Fatal(err)
	}
	r, err := Assess(ctx, c, db)
	if err != nil {
		t.Fatal(err)
	}
	closeTo(t, r.Sites[0].StorageShare, .25)
}

func TestStorageOnlyBudgetWithoutDiskEvidenceRemainsUnknown(t *testing.T) {
	c, _, _, _ := diskFixture()
	c.Host.CostWeights = config.CostWeights{Storage: 1}
	r := Allocate(c, nil, nil)
	if r.Sites[0].Cost != nil || r.Sites[0].CapacityUsage != nil || r.SiteCost != nil || r.MeasuredSites != 0 {
		t.Fatal("missing disk evidence was converted into a priced zero", r)
	}
}
