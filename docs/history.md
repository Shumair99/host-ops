# Local history policy

The workstation/headless SQLite history is separate from the optional VPS service's fixed 30-minute, 256 KiB ring. Live state refreshes at the observation cadence; historical writes are deliberately less frequent.

| Data | History writes | Default retention |
| --- | --- | --- |
| Host resources | Every 15 seconds | 2 days |
| Workload resources | Each 30-second sample | 2 days |
| Health | Immediately on a meaningful change; at most one unchanged report per minute | 7 days |
| Inventory | Only when topology, state or coverage changes | 7 days |
| Change snapshots | Initial state and actual changed fields | 7 days |
| Events, changes, deployments, incidents, alerts, backups, restores | On an observation/transition/import | 365 days |

The latest record of each host/site/kind and the current record of every open incident are preserved. Current health and inventory also remain in `state`; suppressing duplicate historical records does not make the dashboard stale. Changes in response latency alone do not create a health transition. Inventory scans update current health without counting a second public-probe cycle toward incident thresholds.

Optional settings live at the top level of the connection manifest:

```json
"history": {
  "retention_days": 7,
  "metrics_days": 2,
  "evidence_days": 365,
  "max_db_mb": 256
}
```

Omitting settings or using zero selects defaults. Retention accepts 1–3650 days and the database budget accepts 16–65536 MiB. These are maximum retention windows; the size guard can remove older history sooner. If you need permanent evidence, export it to a separately managed private archive.

All database writers share maintenance. Collect-once, agent/watcher cycles and evidence imports apply configured retention before writing. A long-running writer performs age maintenance at least once a minute and checks allocated pages before each write. At 85% of the configured budget it removes oldest telemetry first, then older operational evidence if needed. Protected current records and open incidents survive. A persistent warning records the last pressure-prune time and removed-record count; the dashboard header signals it and `hostops doctor` prints details to stderr. It describes lost older history, rather than a current server outage.

SQLite's page ceiling stops database growth at the budget. Protected evidence that cannot fit causes a clear storage error rather than deleting current incident evidence or consuming unbounded disk. A single unusually large write can fail at that ceiling. WAL/checkpoint files, indexes and one-time migration work need additional disk space: `max_db_mb` is a main database page budget, not a promise that all temporary files total exactly that amount. WAL autocheckpointing and a 1 MiB retained-journal setting limit routine overhead; a transaction can temporarily grow WAL beyond that setting. Independent processes should share one collector per database.

Migrations read the schema version and apply ordered transactional upgrades. A newer unsupported schema is refused. Existing databases receive one compaction pass to enable automatic page reclamation; this may take time and temporarily need roughly another database-sized file. Keep a SQLite-aware backup and sufficient free space before upgrading a large history database. Failed migrations preserve the old transaction rather than silently creating a partial schema.

The one-hour synthetic fixture models 21 sites, a roughly 100 KiB unchanged inventory, public cycles every 30 seconds, host history every 15 seconds and workload history every 30 seconds. It uses 3.13 MiB for that hour, or about 75 MiB/day extrapolated, with one inventory history row. This measures fixture database pages, not real VPS traffic, disk I/O or guaranteed production growth. More sites, larger summaries and frequent changes raise write volume. The configured budget, rather than this extrapolation, is the safety boundary. Actual growth should be measured after deployment.

Deduplication entries remain while their evidence is retained and are removed with pruned records. Older legacy keys without record links expire after the evidence window. Re-importing evidence that has aged out can accept it again; within the retained window, retries remain idempotent.
