package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func TestAutomaticDiskInspectionStartsWithoutOpeningDiskAndDoesNotOverlap(t *testing.T) {
	m := testDashboard(t)
	m.source.Demo = false
	m.page = "overview"
	v, _ := m.Update(tickMsg{})
	m = v.(dashboard)
	if !m.diskReading || m.page != "overview" || m.loading {
		t.Fatal("cold disk did not start in background")
	}
	v, _ = m.Update(tickMsg{})
	m = v.(dashboard)
	if !m.diskReading || m.page != "overview" {
		t.Fatal("pending scan disturbed the page")
	}
	m.diskReading = false
	m.noCollect = true
	v, _ = m.Update(tickMsg{})
	m = v.(dashboard)
	if m.diskReading {
		t.Fatal("--no-collect started a scan")
	}
}
func TestDiskNavigationUsesRetainedTreeAndDoesNotLaunchARead(t *testing.T) {
	m := testDashboard(t)
	disk := DemoDisk()
	m.snapshot.Disk = &disk
	m.page = "disk"
	m.menuFocus = false
	m, cmd := press(m, "enter")
	if cmd != nil || m.currentDiskPath() != "/var" || len(m.diskChildren()) != 1 || m.diskReading {
		t.Fatal("directory navigation scanned or lost the tree")
	}
	m, cmd = press(m, "backspace")
	if cmd != nil || m.currentDiskPath() != "/" {
		t.Fatal("parent navigation failed")
	}
	if !strings.Contains(strings.Join(m.diskLines(120), "\n"), "Property storage") {
		t.Fatal("property storage is absent")
	}
}
func TestOldScanCompletionCannotEraseNewerDiskEvidence(t *testing.T) {
	m := testDashboard(t)
	fresh := metrics.DiskReport{ObservedAt: time.Now(), Directories: []metrics.DiskEntry{{Path: "/new", Bytes: 10}}}
	m.snapshot.Disk = &fresh
	v, _ := m.Update(diskMsg{report: &metrics.DiskReport{ObservedAt: time.Now().Add(-time.Hour)}})
	m = v.(dashboard)
	if m.snapshot.Disk.Directories[0].Path != "/new" {
		t.Fatal("old job erased newer inspection")
	}
	v, _ = m.Update(diskMsg{})
	m = v.(dashboard)
	if m.snapshot.Disk == nil {
		t.Fatal("another owner's pending job erased retained evidence")
	}
}
