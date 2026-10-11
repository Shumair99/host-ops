package tui

import (
	"fmt"
	"path"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Shumair99/host-ops/internal/agent"
	"github.com/Shumair99/host-ops/internal/console"
	"github.com/Shumair99/host-ops/internal/metrics"
)

func (m dashboard) diskCmd(force bool) tea.Cmd {
	return m.jobs.command(func() tea.Msg {
		report, err := agent.InspectDisk(m.ctx, m.source.Config, m.source.Store, force)
		_, scan, stateErr := agent.DiskState(m.ctx, m.source.Store, m.source.Config.Host.ID)
		if err == nil {
			err = stateErr
		}
		return diskMsg{report: report, scan: scan, err: err}
	})
}
func (m dashboard) currentDiskPath() string {
	if m.diskPath != "" {
		return m.diskPath
	}
	if m.snapshot.Disk != nil && len(m.snapshot.Disk.Roots) > 0 {
		return m.snapshot.Disk.Roots[0]
	}
	return "/"
}
func (m dashboard) diskChildren() []metrics.DiskEntry {
	if m.snapshot.Disk == nil {
		return nil
	}
	root := m.currentDiskPath()
	present := map[string]bool{}
	for _, e := range m.snapshot.Disk.Directories {
		present[e.Path] = true
	}
	out := []metrics.DiskEntry{}
	for _, e := range m.snapshot.Disk.Directories {
		parent := e.Parent
		if parent == "" && e.Path != "/" {
			parent = path.Dir(e.Path)
		}
		if e.Path != root && (parent == root || (root == "/" && !present[parent] && e.Path != "/")) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Bytes == out[j].Bytes {
			return out[i].Path < out[j].Path
		}
		return out[i].Bytes > out[j].Bytes
	})
	return out
}
func (m *dashboard) openDiskDirectory() {
	children := m.diskChildren()
	if len(children) == 0 {
		return
	}
	m.diskPath = children[min(m.diskSelected, len(children)-1)].Path
	m.diskSelected, m.offset = 0, 0
}
func diskAmount(n *int64, partial bool) string {
	if n == nil {
		return "—"
	}
	prefix := ""
	if partial {
		prefix = "≥ "
	}
	return prefix + console.Bytes(float64(*n))
}
func (m dashboard) directoryPanel(w int) []string {
	entries := m.diskChildren()
	rows := []string{}
	limit := max(3, min(12, m.bodyHeight()-13))
	selected := min(m.diskSelected, max(0, len(entries)-1))
	start := max(0, selected-limit+1)
	end := min(len(entries), start+limit)
	if len(entries) > 0 {
		rows = append(rows, m.theme.paint(fmt.Sprintf("%d directories · %d–%d", len(entries), start+1, end), muted))
	}
	maximum := int64(0)
	for _, e := range entries {
		maximum = max(maximum, e.Bytes)
	}
	nameWidth := max(8, (w-4)/2)
	barWidth := max(3, w-4-nameWidth-14)
	for i := start; i < end; i++ {
		e := entries[i]
		ratio := 0.0
		if maximum > 0 {
			ratio = float64(e.Bytes) / float64(maximum) * 100
		}
		label := path.Base(e.Path)
		if e.Parent == "" {
			label = e.Path
		}
		line := m.theme.paint(pad(clean(label), nameWidth), ink) + " " + m.theme.meter(&ratio, barWidth, violet) + " " + m.theme.paint(diskAmount(&e.Bytes, e.Partial), violet)
		if i == selected && !m.menuFocus {
			line = m.theme.selection(line, w-4)
		}
		rows = append(rows, clip(line, w-4))
	}
	if len(entries) == 0 {
		rows = append(rows, m.theme.paint("No retained subdirectories", muted))
	}
	for _, e := range m.snapshot.Disk.Directories {
		if e.Path == m.currentDiskPath() {
			rows = append(rows, m.theme.paint("Directory total  "+diskAmount(&e.Bytes, e.Partial), muted))
			break
		}
	}
	return m.theme.panel("Directory explorer / "+clean(m.currentDiskPath()), rows, w)
}
func (m dashboard) siteStoragePanel(w int) []string {
	rows := []string{m.theme.paint(pad("PROPERTY", max(10, w-35))+" "+pad("DIRECTORIES", 13)+" WRITABLE", muted)}
	shared := false
	for _, s := range m.snapshot.Disk.Sites {
		nameWidth := max(10, w-35)
		name := clean(s.Name)
		if s.Shared {
			name = "* " + name
			shared = true
		}
		rows = append(rows, clip(pad(name, nameWidth)+" "+m.theme.paint(pad(diskAmount(s.DirectoryBytes, s.Partial), 13), violet)+" "+m.theme.paint(diskAmount(s.WritableBytes, s.WritablePartial), cyan), w-4))
	}
	if len(m.snapshot.Disk.Sites) == 0 {
		rows = append(rows, m.theme.paint("No observed property storage ownership", muted))
	}
	if shared {
		rows = append(rows, m.theme.paint("* shared storage · rows are not additive", amber))
	}
	return m.theme.panel("Property storage", rows, w)
}
func (m dashboard) diskLines(w int) []string {
	t := m.theme
	n := value(m.snapshot.Host.Values, "disk_percent")
	rows := []string{t.meter(n, max(1, w-14), metricColor(n, amber)) + " " + t.strong(percent(n), metricColor(n, amber)), "Used " + m.byteValue("disk_used_bytes") + "   Free " + m.byteValue("disk_free_bytes") + "   Capacity " + m.byteValue("disk_total_bytes")}
	if at := value(m.snapshot.Host.Values, "disk_observed_unix_seconds"); at != nil {
		rows = append(rows, t.paint("Capacity sample "+ageLabel(time.Unix(int64(*at), 0)), muted))
	}
	out := t.panel("Root filesystem", rows, w)
	if w >= 88 && m.snapshot.Disk != nil {
		a := (w - 1) / 2
		compact := []string{t.meter(n, max(1, a-14), metricColor(n, amber)) + " " + percent(n), "Used " + m.byteValue("disk_used_bytes"), "Free " + m.byteValue("disk_free_bytes"), "Capacity " + m.byteValue("disk_total_bytes")}
		out = columns(t.panel("Root filesystem / current", compact, a), m.storageSignals(w-a-1), a, w-a-1)
	}
	disk := m.snapshot.Disk
	if disk == nil {
		state := "Waiting for automatic inspection"
		if m.noCollect || m.source.Config.Host.DiskScanDisabled {
			state = "Automatic inspection disabled"
		}
		if m.diskReading || time.Now().Before(m.snapshot.DiskScan.RunningUntil) {
			state = "Scanning storage in background…"
		}
		return append(out, t.panel("Storage inspection", []string{t.paint(state, muted)}, w)...)
	}
	at := disk.CompletedAt
	if at.IsZero() {
		at = disk.ObservedAt
	}
	status := fmt.Sprintf("Scanned %s · %.1fs · %d retained directories", ageLabel(at), disk.DurationSeconds, len(disk.Directories))
	if m.diskReading || time.Now().Before(m.snapshot.DiskScan.RunningUntil) {
		status += " · refreshing"
	}
	out = append(out, clip(t.paint(status, muted), w))
	if len(disk.Filesystems) > 0 {
		rows = []string{}
		for _, fs := range disk.Filesystems {
			if !metrics.NormalFilesystem(fs.Type) && fs.Mount != "/" {
				continue
			}
			p := 0.0
			if fs.UsedBytes+fs.AvailableBytes > 0 {
				p = float64(fs.UsedBytes) / float64(fs.UsedBytes+fs.AvailableBytes) * 100
			}
			inode := "—"
			if fs.Inodes > 0 {
				inode = fmt.Sprintf("%.1f%%", float64(fs.InodesUsed)/float64(fs.Inodes)*100)
			}
			data := pad(fs.Type, 7) + " Used " + percent(&p) + "  Free " + console.Bytes(float64(fs.AvailableBytes)) + "  Inodes " + inode
			rows = append(rows, clip(pad(clean(fs.Mount), max(8, w-5-len(data)))+" "+data, w-4))
		}
		out = append(out, t.panel("Filesystems / capacity & inodes", rows, w)...)
	}
	if w >= 88 {
		a := (w - 1) / 2
		out = append(out, columns(m.directoryPanel(a), m.siteStoragePanel(w-a-1), a, w-a-1)...)
	} else {
		out = append(out, m.directoryPanel(w)...)
		out = append(out, m.siteStoragePanel(w)...)
	}
	rows = []string{t.paint(pad("TYPE", max(12, w-32))+" "+pad("SIZE", 10)+" RECLAIMABLE", muted)}
	for _, d := range disk.Docker {
		rows = append(rows, pad(clean(d["Type"]), max(12, w-32))+" "+pad(clean(d["Size"]), 10)+" "+t.paint(clean(d["Reclaimable"]), cyan))
	}
	if len(disk.Docker) == 0 {
		rows = append(rows, t.paint("No Docker accounting available", muted))
	}
	out = append(out, t.panel("Docker storage / shared images & cache", rows, w)...)
	rows = nil
	for _, e := range m.diskChildren() {
		if e.GrowthBytes != nil && *e.GrowthBytes != 0 {
			prefix := "+"
			if *e.GrowthBytes < 0 {
				prefix = "−"
			}
			rows = append(rows, clip(clean(e.Path)+"  "+prefix+console.Bytes(float64(absBytes(*e.GrowthBytes))), w-4))
		}
	}
	if len(rows) > 0 {
		out = append(out, t.panel("Change since previous inspection", rows, w)...)
	}
	if m.snapshot.DiskScan.Issue != "" {
		out = append(out, t.panel("Last refresh", []string{t.paint(clean(m.snapshot.DiskScan.Issue)+" · retained results", amber)}, w)...)
	}
	if len(disk.Unavailable) > 0 {
		rows = []string{t.paint("Partial inspection · some totals are lower bounds", amber)}
		for _, s := range disk.Unavailable {
			rows = append(rows, clean(s))
		}
		out = append(out, t.panel("Coverage", rows, w)...)
	}
	return out
}
func absBytes(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
