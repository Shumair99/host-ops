package agent

import (
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/metrics"
)

func TestDiskScanCadenceUsesCacheAndBacksOffFailures(t *testing.T) {
	now := time.Now()
	fresh := &metrics.DiskReport{CompletedAt: now.Add(-10 * time.Minute)}
	if DiskDue(fresh, DiskScan{}, now) || !DiskDue(nil, DiskScan{}, now) {
		t.Fatal("fresh cache rescanned or cold inspection never starts")
	}
	if DiskDue(nil, DiskScan{RunningUntil: now.Add(time.Minute)}, now) {
		t.Fatal("overlapped a running inspection")
	}
	if DiskDue(nil, DiskScan{StartedAt: now.Add(-time.Minute), Issue: "failed"}, now) {
		t.Fatal("failed inspection retried too quickly")
	}
	if !DiskDue(fresh, DiskScan{StartedAt: now.Add(-6 * time.Minute), Issue: "failed"}, now) {
		t.Fatal("failed inspection never retried")
	}
	if !DiskDue(&metrics.DiskReport{CompletedAt: now.Add(-31 * time.Minute)}, DiskScan{}, now) {
		t.Fatal("old cache did not refresh")
	}
	if !DiskDue(&metrics.DiskReport{ObservedAt: now}, DiskScan{}, now) {
		t.Fatal("legacy shallow inspection prevented richer scan")
	}
	if !DiskDue(nil, DiskScan{StartedAt: now.Add(-90 * time.Second), RunningUntil: now.Add(-15 * time.Second)}, now) {
		t.Fatal("interrupted inspection did not retry after lease expiry")
	}
}
