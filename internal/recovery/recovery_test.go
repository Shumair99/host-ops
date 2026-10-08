package recovery

import (
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"testing"
	"time"
)

func TestBackupDoesNotProveRestoreAndOldEvidenceFailsRPO(t *testing.T) {
	now := time.Now()
	b := &Backup{Status: "succeeded", CompletedAt: now.Add(-48 * time.Hour)}
	a := Evaluate(config.Site{}, b, nil, now)
	if a.RestoreState != model.Unknown || a.RPOstate != model.Fail || a.Confidence != "unverified" {
		t.Fatal(a)
	}
	r := Restore{ID: "r", SiteID: "s", BackupID: "b", Status: "verified", StartedAt: now.Add(-time.Minute), CompletedAt: now, RecoveredPointAt: now}
	if r.Validate(now) == nil {
		t.Fatal("unverified restore accepted")
	}
	r.HealthVerified = true
	r.IsolationVerified = true
	r.EvidenceReference = "restore-artifact-1"
	if r.Validate(now) != nil {
		t.Fatal("valid reported evidence rejected")
	}
}
