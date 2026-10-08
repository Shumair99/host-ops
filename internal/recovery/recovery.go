package recovery

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Shumair99/host-ops/internal/config"
	"github.com/Shumair99/host-ops/internal/model"
	"github.com/Shumair99/host-ops/internal/storage"
	"regexp"
	"time"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

type Backup struct {
	HostID      string    `json:"host_id"`
	SiteID      string    `json:"site_id"`
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	SizeBytes   int64     `json:"size_bytes"`
	CompletedAt time.Time `json:"completed_at"`
	Destination string    `json:"destination"`
	Origin      string    `json:"origin"`
	ObservedAt  time.Time `json:"observed_at"`
}
type Restore struct {
	HostID             string    `json:"host_id"`
	SiteID             string    `json:"site_id"`
	ID                 string    `json:"id"`
	BackupID           string    `json:"backup_id"`
	Status             string    `json:"status"`
	StartedAt          time.Time `json:"started_at"`
	CompletedAt        time.Time `json:"completed_at"`
	RecoveredPointAt   time.Time `json:"recovered_point_at"`
	HealthVerified     bool      `json:"health_verified"`
	IsolationVerified  bool      `json:"isolation_verified"`
	MeasuredRTOseconds float64   `json:"measured_rto_seconds"`
	EvidenceReference  string    `json:"evidence_reference"`
	Origin             string    `json:"origin"`
	ObservedAt         time.Time `json:"observed_at"`
}

func (b Backup) Validate(now time.Time) error {
	if !identifier.MatchString(b.ID) || !identifier.MatchString(b.SiteID) {
		return fmt.Errorf("valid backup id and site_id required")
	}
	if b.Status != "succeeded" && b.Status != "failed" && b.Status != "in_progress" {
		return fmt.Errorf("invalid backup status")
	}
	if b.SizeBytes < 0 {
		return fmt.Errorf("backup size cannot be negative")
	}
	if b.Status != "in_progress" && (b.CompletedAt.IsZero() || b.CompletedAt.After(now.Add(time.Minute))) {
		return fmt.Errorf("valid completed_at required")
	}
	if b.Destination != "local" && b.Destination != "offsite" && b.Destination != "unknown" {
		return fmt.Errorf("destination must be local, offsite or unknown")
	}
	return nil
}
func (r Restore) Validate(now time.Time) error {
	if !identifier.MatchString(r.ID) || !identifier.MatchString(r.SiteID) || !identifier.MatchString(r.BackupID) {
		return fmt.Errorf("valid restore id, backup_id and site_id required")
	}
	if r.Status != "verified" && r.Status != "failed" {
		return fmt.Errorf("restore status must be verified or failed")
	}
	if r.StartedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) || r.CompletedAt.After(now.Add(time.Minute)) {
		return fmt.Errorf("valid restore chronology required")
	}
	if r.Status == "verified" && (!r.HealthVerified || !r.IsolationVerified || r.EvidenceReference == "") {
		return fmt.Errorf("verified restore requires health, isolation and an evidence reference")
	}
	if r.RecoveredPointAt.IsZero() || r.RecoveredPointAt.After(r.CompletedAt) {
		return fmt.Errorf("valid recovered data point required")
	}
	if len(r.EvidenceReference) > 256 || regexp.MustCompile(`[\r\n?@]`).MatchString(r.EvidenceReference) {
		return fmt.Errorf("evidence_reference must be a short artifact identifier without URL credentials or queries")
	}
	return nil
}

type Assessment struct {
	HostID       string      `json:"host_id"`
	SiteID       string      `json:"site_id"`
	Name         string      `json:"name"`
	RPOhours     int         `json:"rpo_target_hours"`
	RTOminutes   int         `json:"rto_target_minutes"`
	BackupState  model.State `json:"backup_state"`
	RestoreState model.State `json:"restore_state"`
	RPOstate     model.State `json:"rpo_state"`
	RTOstate     model.State `json:"rto_state"`
	LastBackup   *Backup     `json:"last_backup"`
	LastRestore  *Restore    `json:"last_restore"`
	Confidence   string      `json:"confidence"`
	Notes        []string    `json:"notes"`
}

func Evaluate(site config.Site, backup *Backup, restore *Restore, now time.Time) Assessment {
	a := Assessment{HostID: site.HostID, SiteID: site.ID, Name: site.Name, RPOhours: site.Recovery.RPOhours, RTOminutes: site.Recovery.RTOminutes, BackupState: model.Unknown, RestoreState: model.Unknown, RPOstate: model.Unknown, RTOstate: model.Unknown, LastBackup: backup, LastRestore: restore, Confidence: "unverified", Notes: []string{"Recovery records are operator/pipeline-reported evidence. HostOps does not execute restores or independently inspect the referenced artifact.", "A successful backup alone never establishes restore capability."}}
	if a.RPOhours == 0 {
		a.RPOhours = 24
	}
	if a.RTOminutes == 0 {
		a.RTOminutes = 15
	}
	if backup != nil {
		a.BackupState = model.Warn
		if backup.Status == "failed" {
			a.BackupState = model.Fail
		}
		if backup.Status == "succeeded" {
			a.BackupState = model.Pass
			a.RPOstate = model.Pass
			if now.Sub(backup.CompletedAt) > time.Duration(a.RPOhours)*time.Hour {
				a.RPOstate = model.Fail
			}
		}
	}
	if restore != nil {
		a.RestoreState = model.Fail
		if restore.Status == "verified" && restore.HealthVerified && restore.IsolationVerified {
			a.RestoreState = model.Pass
			a.Confidence = "reported_verified"
			if now.Sub(restore.CompletedAt) > 30*24*time.Hour {
				a.RestoreState = model.Warn
			}
			a.RTOstate = model.Pass
			if restore.CompletedAt.Sub(restore.StartedAt) > time.Duration(a.RTOminutes)*time.Minute {
				a.RTOstate = model.Fail
			}
		}
	}
	return a
}
func Assess(ctx context.Context, c config.Config, site config.Site, store *storage.Store) (Assessment, error) {
	var backup *Backup
	var restore *Restore
	b, err := store.Query(ctx, c.Host.ID, site.ID, "backup", time.Time{}, 1)
	if err != nil {
		return Assessment{}, err
	}
	if len(b) > 0 {
		backup = &Backup{}
		if err = json.Unmarshal(b[0].Data, backup); err != nil {
			return Assessment{}, err
		}
	}
	r, err := store.Query(ctx, c.Host.ID, site.ID, "restore", time.Time{}, 1)
	if err != nil {
		return Assessment{}, err
	}
	if len(r) > 0 {
		restore = &Restore{}
		if err = json.Unmarshal(r[0].Data, restore); err != nil {
			return Assessment{}, err
		}
	}
	return Evaluate(site, backup, restore, time.Now()), nil
}
