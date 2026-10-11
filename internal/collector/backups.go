package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Shumair99/host-ops/internal/discovery"
	"github.com/Shumair99/host-ops/internal/recovery"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func backupFormat(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("SQLite format 3\x00")):
		return "SQLite copy"
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b, 0x08}):
		return "gzip archive"
	case bytes.HasPrefix(data, []byte{'P', 'K', 3, 4}):
		return "ZIP archive"
	case len(data) > 262 && bytes.Equal(data[257:262], []byte("ustar")):
		return "tar archive"
	}
	return ""
}

// Bounded scans of backup-named folders in recorded Compose directories and
// persistent mounts. No symlinks, live database reads or archive extraction.
// Signatures establish artifact type, not backup success or restore capability.
func readBackupArtifacts(ctx context.Context, snapshot *discovery.Snapshot) {
	artifacts := map[string]bool{}
	for _, w := range snapshot.Workloads {
		roots := []string{w.Directory}
		for _, m := range w.Mounts {
			roots = append(roots, m.Source)
		}
		seen := map[string]bool{}
		for _, root := range roots {
			root = filepath.Clean(root)
			if !filepath.IsAbs(root) || root == string(filepath.Separator) || seen[root] {
				continue
			}
			seen[root] = true
			count := 0
			_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if ctx.Err() != nil {
					return fs.SkipAll
				}
				count++
				if count > 4096 {
					snapshot.BackupIssues = append(snapshot.BackupIssues, w.Name+": artifact scan limit reached")
					return fs.SkipAll
				}
				if err != nil {
					snapshot.BackupIssues = append(snapshot.BackupIssues, w.Name+": backup location unreadable")
					return nil
				}
				rel, _ := filepath.Rel(root, path)
				parts := strings.Split(filepath.ToSlash(rel), "/")
				if entry.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if entry.IsDir() {
					if len(parts) > 4 || entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == ".next" {
						return fs.SkipDir
					}
					return nil
				}
				inBackup := backupDirectory(filepath.Base(root))
				for _, part := range parts[:len(parts)-1] {
					inBackup = inBackup || backupDirectory(part)
				}
				if !inBackup || !entry.Type().IsRegular() {
					return nil
				}
				info, e := entry.Info()
				if e != nil || info.Size() < 16 {
					return nil
				}
				file, e := os.Open(path)
				if e != nil {
					return nil
				}
				header := make([]byte, 512)
				n, _ := file.Read(header)
				_ = file.Close()
				format := backupFormat(header[:n])
				if format == "" {
					return nil
				}
				key := w.ID + "|" + path
				if artifacts[key] {
					return nil
				}
				artifacts[key] = true
				if len(snapshot.BackupArtifacts) >= 256 {
					return fs.SkipAll
				}
				snapshot.BackupArtifacts = append(snapshot.BackupArtifacts, discovery.BackupArtifact{WorkloadID: w.ID, Container: w.Name, Path: path, Format: format, SizeBytes: info.Size(), ModifiedAt: info.ModTime().UTC()})
				return nil
			})
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	readRecoveryReports(filepath.Join(home, ".local", "state", "hostops", "recovery"), snapshot)
}

func backupDirectory(name string) bool {
	name = strings.ToLower(name)
	return name == "backup" || name == "backups" || name == "incident-backups" || strings.HasPrefix(name, "backup-")
}

func readRecoveryReports(root string, snapshot *discovery.Snapshot) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		snapshot.BackupIssues = append(snapshot.BackupIssues, "Automatic recovery report directory unreadable")
		return
	}
	for i, entry := range entries {
		if i >= 256 {
			snapshot.BackupIssues = append(snapshot.BackupIssues, "Recovery report limit reached")
			break
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		f, e := os.Open(path)
		if e != nil {
			continue
		}
		data, e := io.ReadAll(io.LimitReader(f, 65537))
		_ = f.Close()
		var report discovery.RecoveryReport
		if e != nil || len(data) > 65536 || json.Unmarshal(data, &report) != nil || report.Version != 1 || report.Kind != "backup" && report.Kind != "restore" || len(report.Domains) == 0 || len(report.Domains) > 64 {
			snapshot.BackupIssues = append(snapshot.BackupIssues, "Invalid recovery report: "+entry.Name())
			continue
		}
		valid := true
		for _, domain := range report.Domains {
			valid = valid && discovery.ConcreteHost(domain)
		}
		if !valid {
			continue
		}
		// Decode and re-encode a strict public projection; arbitrary extra fields
		// and job credentials must never be retained in inventory/history.
		if report.Kind == "backup" {
			var record recovery.Backup
			if json.Unmarshal(report.Record, &record) != nil {
				continue
			}
			record.HostID = snapshot.HostID
			record.SiteID = "reported"
			record.Origin = "automatic-job-report"
			if record.Validate(snapshot.ObservedAt) != nil {
				continue
			}
			report.Record, _ = json.Marshal(record)
		} else {
			var record recovery.Restore
			if json.Unmarshal(report.Record, &record) != nil {
				continue
			}
			record.HostID = snapshot.HostID
			record.SiteID = "reported"
			record.Origin = "automatic-job-report"
			if record.Validate(snapshot.ObservedAt) != nil {
				continue
			}
			report.Record, _ = json.Marshal(record)
		}
		snapshot.RecoveryReports = append(snapshot.RecoveryReports, report)
	}
}
