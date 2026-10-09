package recovery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
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

func validBackup(now time.Time) Backup {
	return Backup{ID: "backup-1", SiteID: "site-1", Status: "succeeded", SizeBytes: 1024, CompletedAt: now.Add(-time.Minute), Destination: "offsite"}
}

func validRestore(now time.Time) Restore {
	return Restore{
		ID: "restore-1", SiteID: "site-1", BackupID: "backup-1", Status: "verified",
		StartedAt: now.Add(-10 * time.Minute), CompletedAt: now,
		RecoveredPointAt: now.Add(-5 * time.Minute), HealthVerified: true,
		IsolationVerified: true, EvidenceReference: "restore-artifact-1",
	}
}

func TestBackupValidationRules(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*Backup)
	}{
		{name: "invalid id", mutate: func(b *Backup) { b.ID = "../backup" }},
		{name: "invalid site", mutate: func(b *Backup) { b.SiteID = "site with spaces" }},
		{name: "invalid status", mutate: func(b *Backup) { b.Status = "complete" }},
		{name: "negative size", mutate: func(b *Backup) { b.SizeBytes = -1 }},
		{name: "missing completion", mutate: func(b *Backup) { b.CompletedAt = time.Time{} }},
		{name: "future completion", mutate: func(b *Backup) { b.CompletedAt = now.Add(2 * time.Minute) }},
		{name: "invalid destination", mutate: func(b *Backup) { b.Destination = "remote-url" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := validBackup(now)
			tc.mutate(&b)
			if err := b.Validate(now); err == nil {
				t.Fatal("invalid backup evidence accepted")
			}
		})
	}

	for _, b := range []Backup{
		validBackup(now),
		{ID: "backup-2", SiteID: "site-1", Status: "in_progress", Destination: "unknown"},
		{ID: "backup-3", SiteID: "site-1", Status: "failed", CompletedAt: now.Add(time.Minute), Destination: "local"},
	} {
		if err := b.Validate(now); err != nil {
			t.Fatalf("valid %s backup evidence rejected: %v", b.Status, err)
		}
	}
}

func TestRestoreValidationRules(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		mutate func(*Restore)
	}{
		{name: "invalid id", mutate: func(r *Restore) { r.ID = "../restore" }},
		{name: "invalid site", mutate: func(r *Restore) { r.SiteID = "site with spaces" }},
		{name: "invalid backup id", mutate: func(r *Restore) { r.BackupID = "backup with spaces" }},
		{name: "invalid status", mutate: func(r *Restore) { r.Status = "succeeded" }},
		{name: "missing start", mutate: func(r *Restore) { r.StartedAt = time.Time{} }},
		{name: "completion before start", mutate: func(r *Restore) { r.CompletedAt = r.StartedAt.Add(-time.Second) }},
		{name: "future completion", mutate: func(r *Restore) { r.CompletedAt = now.Add(2 * time.Minute) }},
		{name: "missing health proof", mutate: func(r *Restore) { r.HealthVerified = false }},
		{name: "missing isolation proof", mutate: func(r *Restore) { r.IsolationVerified = false }},
		{name: "missing reference", mutate: func(r *Restore) { r.EvidenceReference = "" }},
		{name: "missing recovered point", mutate: func(r *Restore) { r.RecoveredPointAt = time.Time{} }},
		{name: "recovered point after completion", mutate: func(r *Restore) { r.RecoveredPointAt = r.CompletedAt.Add(time.Second) }},
		{name: "long reference", mutate: func(r *Restore) { r.EvidenceReference = strings.Repeat("x", 257) }},
		{name: "newline reference", mutate: func(r *Restore) { r.EvidenceReference = "artifact\nnext" }},
		{name: "query reference", mutate: func(r *Restore) { r.EvidenceReference = "artifact?token=value" }},
		{name: "credential marker", mutate: func(r *Restore) { r.EvidenceReference = "user@artifact" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validRestore(now)
			tc.mutate(&r)
			if err := r.Validate(now); err == nil {
				t.Fatal("invalid restore evidence accepted")
			}
		})
	}

	valid := validRestore(now)
	if err := valid.Validate(now); err != nil {
		t.Fatalf("verified restore evidence rejected: %v", err)
	}
	failed := valid
	failed.Status, failed.HealthVerified, failed.IsolationVerified, failed.EvidenceReference = "failed", false, false, ""
	if err := failed.Validate(now); err != nil {
		t.Fatalf("failed restore without success claims rejected: %v", err)
	}
}

func TestAssessReadsEvidenceAndPropagatesClosedStoreError(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	c := config.Config{Host: config.Host{ID: "host-1"}}
	site := config.Site{ID: "site-1", Name: "Example", Recovery: config.Recovery{RPOhours: 12, RTOminutes: 20}}
	assessment, err := Assess(ctx, c, site, store)
	if err != nil || assessment.BackupState != model.Unknown || assessment.RestoreState != model.Unknown {
		t.Fatalf("missing evidence was not unknown: assessment=%+v err=%v", assessment, err)
	}
	if assessment.RPOhours != 12 || assessment.RTOminutes != 20 {
		t.Fatalf("configured recovery targets were not retained: %+v", assessment)
	}
	now := time.Now().UTC()
	backup, restore := validBackup(now), validRestore(now)
	if _, err = store.Add(ctx, c.Host.ID, site.ID, "backup", now, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Add(ctx, c.Host.ID, site.ID, "restore", now, restore); err != nil {
		t.Fatal(err)
	}
	assessment, err = Assess(ctx, c, site, store)
	if err != nil || assessment.BackupState != model.Pass || assessment.RestoreState != model.Pass {
		t.Fatalf("saved evidence was not reflected: assessment=%+v err=%v", assessment, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Assess(ctx, c, site, store); err == nil {
		t.Fatal("closed storage error was swallowed")
	}
}
