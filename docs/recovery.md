# Backup and reported restoration evidence

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
