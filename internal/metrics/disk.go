package metrics

import (
	"context"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/platform"
)

type DiskEntry struct {
	Path        string `json:"path"`
	Parent      string `json:"parent,omitempty"`
	Bytes       int64  `json:"bytes"`
	Partial     bool   `json:"partial"`
	GrowthBytes *int64 `json:"growth_bytes,omitempty"`
}
type Filesystem struct {
	Mount          string `json:"mount"`
	Type           string `json:"type"`
	TotalBytes     int64  `json:"total_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
	Inodes         int64  `json:"inodes"`
	InodesUsed     int64  `json:"inodes_used"`
}
type SiteStorage struct {
	SiteID          string   `json:"site_id"`
	Name            string   `json:"name"`
	Paths           []string `json:"paths"`
	DirectoryBytes  *int64   `json:"directory_bytes,omitempty"`
	WritableBytes   *int64   `json:"writable_bytes,omitempty"`
	WritablePartial bool     `json:"writable_partial,omitempty"`
	Shared          bool     `json:"shared"`
	Partial         bool     `json:"partial"`
}
type DiskReport struct {
	HostID          string              `json:"host_id"`
	ObservedAt      time.Time           `json:"observed_at"`
	Directories     []DiskEntry         `json:"directories"`
	Docker          []map[string]string `json:"docker"`
	Unavailable     []string            `json:"unavailable"`
	CompletedAt     time.Time           `json:"completed_at,omitempty"`
	DurationSeconds float64             `json:"duration_seconds,omitempty"`
	Roots           []string            `json:"roots,omitempty"`
	Filesystems     []Filesystem        `json:"filesystems,omitempty"`
	Sites           []SiteStorage       `json:"sites,omitempty"`
}

func Disk(ctx context.Context, runner platform.Runner, h config.Host) DiskReport {
	return DiskWithInventory(ctx, runner, h, discovery.Registry{}, nil)
}
func DiskWithInventory(ctx context.Context, runner platform.Runner, h config.Host, inventory discovery.Registry, sites []config.Site) DiskReport {
	return inspectDisk(ctx, runner, h, inventory, sites)
}
