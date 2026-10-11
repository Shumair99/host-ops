package costs

import (
	"strings"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/metrics"
)

// Disk accounting runs every thirty minutes, independently of compute samples.
const storageMaxAge = 35 * time.Minute

type storageEvidence struct {
	report *metrics.DiskReport
	root   metrics.Filesystem
	issue  string
}

func newStorageEvidence(report *metrics.DiskReport, host string, now time.Time) storageEvidence {
	e := storageEvidence{issue: "Storage accounting unavailable; open Disk usage to inspect or refresh the retained scan. Missing site storage is not zero."}
	if report == nil || report.HostID != host {
		return e
	}
	if report.ObservedAt.IsZero() || report.ObservedAt.Before(now.Add(-storageMaxAge)) || report.ObservedAt.After(now.Add(time.Minute)) {
		e.issue = "Storage accounting is stale or invalid; site storage remains unknown until the next disk inspection."
		return e
	}
	for _, fs := range report.Filesystems {
		if fs.Mount == "/" && metrics.NormalFilesystem(fs.Type) && fs.TotalBytes > 0 && fs.UsedBytes >= 0 && fs.UsedBytes <= fs.TotalBytes {
			e.report, e.root, e.issue = report, fs, ""
			return e
		}
	}
	return e
}

func (e storageEvidence) host(r *Report) {
	if e.report == nil {
		r.Notes = append(r.Notes, e.issue)
		return
	}
	r.HostStorageUsage = number(float64(e.root.UsedBytes) / float64(e.root.TotalBytes))
	r.StorageTotalBytes, r.StorageUsedBytes = &e.root.TotalBytes, &e.root.UsedBytes
	r.StorageObservedAt = e.report.ObservedAt
	r.Notes = append(r.Notes, "Storage is a retained root-filesystem snapshot, not a month of disk history. Other filesystems and unassignable shared storage are not charged to individual sites.")
}

func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/") || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/")
}

func (e storageEvidence) site(a *Allocation, g allocationGroup) {
	if e.report == nil {
		a.StorageNote = e.issue
		return
	}
	ids := map[string]bool{}
	for _, id := range g.IDs {
		ids[id] = true
	}
	var selected *metrics.SiteStorage
	for i := range e.report.Sites {
		item := &e.report.Sites[i]
		if !ids[item.SiteID] {
			continue
		}
		if selected == nil || item.SiteID == g.Site.ID {
			selected = item
		}
	}
	if selected == nil {
		a.StorageNote = "No attributable storage measurement; retained disk scan has no matching site."
		return
	}
	item := *selected
	if item.Shared {
		a.StorageNote = "Shared storage remains unassigned rather than charged to each site."
		return
	}
	for _, other := range e.report.Sites {
		if ids[other.SiteID] {
			continue
		}
		for _, p := range item.Paths {
			for _, q := range other.Paths {
				if overlaps(p, q) {
					a.StorageNote = "Overlapping directory ownership remains shared/unassigned."
					return
				}
			}
		}
	}
	bytes, known, partial := int64(0), false, item.Partial || item.WritablePartial
	add := func(n int64) bool {
		if n < 0 || n > e.root.UsedBytes || bytes > e.root.UsedBytes-n {
			return false
		}
		bytes += n
		known = true
		return true
	}
	if item.DirectoryBytes != nil {
		rootPaths, otherPaths, unknownPaths := 0, 0, 0
		for _, p := range item.Paths {
			switch metrics.StorageFilesystem(p, e.report.Filesystems) {
			case "/":
				rootPaths++
			case "":
				unknownPaths++
			default:
				otherPaths++
			}
		}
		if len(item.Paths) > 0 && unknownPaths == 0 && otherPaths == 0 {
			if !add(*item.DirectoryBytes) {
				a.StorageNote = "Storage measurement exceeds used disk capacity or is invalid."
				return
			}
		} else if rootPaths == 0 && unknownPaths == 0 && otherPaths > 0 {
			known = true // Observed directories are wholly on another filesystem.
		} else {
			partial = true
		}
	} else if len(item.Paths) > 0 {
		partial = true
	}
	if item.WritableBytes != nil {
		switch item.WritableFilesystem {
		case "/":
			if !add(*item.WritableBytes) {
				a.StorageNote = "Storage measurement exceeds used disk capacity or is invalid."
				return
			}
		case "":
			partial = true
		default:
			known = true // Observed layer is on another filesystem.
		}
	} else if g.Site.Runtime.Type == "docker" || g.Site.Runtime.Type == "podman" {
		partial = true
	}
	if !known {
		a.StorageNote = "Storage ownership or filesystem is not measured; no disk share is assumed."
		return
	}
	a.StorageBytes, a.StorageShare = &bytes, number(float64(bytes)/float64(e.root.TotalBytes))
	a.UsagePartial = partial
	if partial {
		a.StorageNote = "Measured root-disk footprint is a lower bound; missing paths/layers are unassigned."
	}
}

func combined(cpu, memory, disk *float64, w config.CostWeights) *float64 {
	n := weighted(cpu, memory, w)
	if n == nil || w.Storage > 0 && disk == nil {
		return nil
	}
	if disk != nil {
		*n += w.Storage * *disk
	}
	return n
}
