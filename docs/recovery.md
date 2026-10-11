# Backup and reported restoration evidence

In the dashboard, open **Tools → Recovery** and press **S**. Choose **Import backup report**, **Import restore test report**, or **Scan existing backups again**. Imports read an existing JSON job report from this computer and use the same validation and site scoping as the CLI below. A scan discovers readable existing artifacts and reports; it does not create backups or claim that restoring them works.

`hostops record backup --file FILE` accepts categorized backup status, size, completion timestamp and destination (local/offsite/unknown). `hostops record restore --file FILE` accepts results from an operator or existing isolated restoration workflow. They use the same bounded JSON validation, host/site scoping and idempotency as deployment records. HostOps assigns the `operator_report` origin; no backup contents, credentials or remote storage URLs are accepted.

```json
{"site_id":"client-a","id":"backup-1","status":"succeeded","size_bytes":1048576,"completed_at":"2026-10-08T00:00:00Z","destination":"offsite"}
```

```json
{"site_id":"client-a","id":"restore-1","backup_id":"backup-1","status":"verified","started_at":"2026-10-08T00:10:00Z","completed_at":"2026-10-08T00:16:41Z","recovered_point_at":"2026-10-08T00:00:00Z","health_verified":true,"isolation_verified":true,"evidence_reference":"restore-artifact-1"}
```

Verified records require declared health verification, isolation, an artifact reference and valid chronology. HostOps derives measured duration from timestamps. It does not execute destructive restoration, independently inspect the artifact or turn a reported claim into independently proven recovery. Confidence therefore says `reported_verified`. RPO uses backup completion time, not the time an old record was submitted. A backup alone never passes restore verification. RTO uses the reported measured restoration duration. Targets default to 24 hours RPO and 15 minutes RTO when unspecified.

Save real reports to local files, using a registered site ID and actual timestamps. Then:

```powershell
.\bin\hostops.exe record backup --config local/hostops.json --file local/backup.json
.\bin\hostops.exe record restore --config local/hostops.json --file local/restore.json
.\bin\hostops.exe recovery --config local/hostops.json
```

The terminal Recovery view reads the same evidence. The sample reports above are format examples, not verified backups or restores.

Find the exact current ID using `hostops sites --config local/hostops.json --no-collect --json`. Site names are display labels; reports must use `site_id`. Records are scoped to that site and database. Related domains or shared containers do not automatically establish that a backup covers another site's data. If your dashboard uses `--db`, pass the same database path to both imports. A VPS/CI backup job can publish the automatic report described below, or import its report on the machine storing HostOps history. No public receiver is exposed.

For a safe workflow, restore into a separate test database/volume, verify application health, then record the actual test result and timing. Merely finding a backup file does not qualify as a successful backup job or verified restoration. See [the operations tools guide](operations-tools.md) for the meaning of RPO, RTO, unknown states and confidence.

## Automatic discovery

The collector checks recorded Compose working directories and persistent mounts for backup folders. It recognizes SQLite copies and gzip, ZIP and tar headers, without reading database rows or extracting archives. Scans are bounded and skip symlinks. A recognized artifact shows its location, size and modification time; it does not establish that a backup job succeeded, that the file covers the whole site, or that it can be restored. Unreadable locations and collection limits appear as coverage gaps. Remote/provider snapshots and repository formats such as restic or Borg are not inferred from unrelated files.

The single authoritative automatic status strategy is a small report written by the actual backup or isolated restore job. The collector reads JSON files from `~/.local/state/hostops/recovery/` on the VPS, under the SSH account used by HostOps. Use a private directory (0700), files (0600), and atomic rename after writing. Keep the latest result for each job/site so the bounded directory does not accumulate more than 256 files.

A backup job report has this envelope. Replace the example times with real completion times; publish success only after the backup tool returns success and the intended destination has been checked:

```json
{
  "version": 1,
  "kind": "backup",
  "domains": ["example.com"],
  "record": {
    "id": "nightly-20261010",
    "status": "succeeded",
    "size_bytes": 1048576,
    "completed_at": "2026-10-10T01:00:00Z",
    "destination": "offsite"
  }
}
```

A restore job uses the same envelope with `kind: "restore"`. Its `record` uses the restore fields at the top of this document. A verified result requires an actual isolated restoration, a health check, its backup identifier, real start/completion/recovered-point times and a short artifact reference. Domain scope must list the addresses whose data the job actually protects; sharing a Docker network does not establish backup coverage. The collector supplies host/site scoping, validates chronology and strips unsupported fields. Never put credentials, database contents or storage URLs into a report.

Reports update Recovery, Readiness and Pre-flight on the next collection cycle. Confidence remains `reported_verified`: HostOps has validated a job's report, rather than independently performed the restoration. A discovered file alone leaves RPO, restore capability and RTO unknown. Manual imports continue to work against the same history database.
