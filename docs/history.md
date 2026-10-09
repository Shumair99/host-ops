# Local history policy

The workstation/headless SQLite history is separate from the optional VPS service's fixed 30-minute, 256 KiB ring. Live state refreshes at the observation cadence; historical writes are deliberately less frequent.

| Data | History writes | Default retention |
| --- | --- | --- |
| Host resources | Every 15 seconds | Detailed 24h; 5-minute summaries through day 7; 30-minute summaries through day 30 |
| Workload resources | Each 30-second sample | Detailed 24h; 5-minute summaries through day 7; 30-minute summaries through day 30 |
| Health | Immediately on a meaningful change; at most one unchanged report per minute | 7 days |
| Inventory | Only when topology, state or coverage changes | 7 days |
| Change snapshots | Initial state and actual changed fields | 7 days |
| Events, changes, deployments, incidents, alerts, backups, restores | On an observation/transition/import | 365 days |

The latest raw record of each host/site/kind and the current record of every open incident are preserved. Expired metric summaries are removed, including their latest window. Current health and inventory also remain in `state`; suppressing duplicate historical records does not make the dashboard stale. Changes in response latency alone do not create a health transition. Inventory scans update current health without counting a second public-probe cycle toward incident thresholds.

The default `tiered` policy consolidates older resource measurements automatically during ordinary writer maintenance. Recent detail and live collection stay unchanged. Each metric's summary keeps its measured minimum, maximum, sample-weighted mean, known-value count, first/last values and exact measured endpoint times. It also keeps seconds with actual observations and counts of unavailable reasons. No window is created for an empty interval; missing values never become zero. Peaks survive averaging, but the individual timing/order and intermediate values of older readings are intentionally no longer retained. Only complete age windows close, allowing up to five minutes of grace at the detailed boundary and thirty minutes at older boundaries. An unrecognized/custom metric record stays raw through `metrics_days` instead of being interpreted destructively.

Optional settings live at the top level of the connection manifest:

```json
"history": {
  "retention_days": 7,
  "metrics_days": 30,
  "metric_detail": "tiered",
  "evidence_days": 365,
  "max_db_mb": 256
}
```

Omitting settings or using zero selects defaults. `metrics_days` is the total metric horizon; an explicit shorter horizon is honored. Set `metric_detail` to `full` to keep individual readings through that horizon. With `full` and no explicit `metrics_days`, the previous two-day default applies. Switching to full cannot reconstruct earlier summarized readings; existing summaries keep aging through the configured horizon. Retention accepts 1–3650 days and the database budget accepts 16–65536 MiB. The size guard can remove older history sooner. If you need permanent evidence, export it to a separately managed private archive.

All database writers share maintenance. Collect-once, agent/watcher cycles and evidence imports apply configured retention before writing. A long-running writer performs age maintenance at least once a minute and checks allocated pages before each write. At 85% of the configured budget it removes oldest telemetry first, then older operational evidence if needed. Protected current records and open incidents survive. A persistent warning records the last pressure-prune time and removed-record count; the dashboard header signals it and `hostops doctor` prints details to stderr. It describes lost older history, rather than a current server outage.

SQLite's page ceiling stops database growth at the budget. Protected evidence that cannot fit causes a clear storage error rather than deleting current incident evidence or consuming unbounded disk. A single unusually large write can fail at that ceiling. WAL/checkpoint files, indexes and one-time migration work need additional disk space: `max_db_mb` is a main database page budget, not a promise that all temporary files total exactly that amount. WAL autocheckpointing and a 1 MiB retained-journal setting limit routine overhead; a transaction can temporarily grow WAL beyond that setting. Independent processes should share one collector per database.

Migrations read the schema version and apply ordered transactional upgrades. A newer unsupported schema is refused. Schema 4 losslessly converts legacy records and current state to compact blocks, retaining record IDs and evidence keys. Schema 5 permits 512-sample blocks and resumable compressed summary blocks without rewriting version 4 frames. Close older HostOps processes before launching the upgraded version. Legacy conversion may take time and temporarily need roughly another database-sized file; keep a SQLite-aware backup and sufficient free space for a large upgrade. Failed conversion transactions preserve the original schema/data. Older executables cannot read schema 5.

History uses one stream identity per host/site/kind, bounded independent blocks, exact JSON/numeric/timestamp deltas and compression. Current state is compressed separately. The lossless format preserves every raw JSON byte, record ID and nanosecond timestamp. Summarization is a separate, explicit detail policy. Summary creation and source removal commit together; errors leave original readings intact. Late individual imports into an already summarized window are refused, preventing replay from double-counting its statistics. Finalized payloads are separate from growing blocks to avoid page fragmentation. SQLite uses 1 KiB pages. See [storage efficiency](storage-efficiency.md) for measurements.

The full-day lossless fixture with 21 sites and 96,480 records now uses 0.280 MiB for stable data and 1.546 MiB for changing resource/health data, versus 61.730/76.766 MiB in schema 3. A separate resource-only fixture accounts for all 66,240 readings: 1.143 MiB at full detail, 0.537 MiB after five-minute summarization, 0.182 MiB after thirty-minute summarization. These are actual physical database totals for a single day's data at different ages, not complete production daily-growth guarantees. Health, inventory, operational events, current state and temporary files add storage. The configured budget remains the safety boundary.

Read retained resource history without SSH with `hostops metrics --history --since 720h --json`. Add a site ID for workload history. `--resolution raw`, `5m` or `30m` selects a saved tier; `auto` combines tiers and folds a protected raw tail into its returned summary without recounting it. Results contain up to the newest 1,000 points and return intersecting summary windows whole. The regular `metrics` command still performs its live probe. Capacity percentiles use individual recent readings; disk forecasts can use an older summary's actual first measured endpoint.

Operational evidence deduplication entries remain while their records are retained and are removed with pruned observations. Older legacy keys without record links expire after the evidence window. Timestamp replay deduplication checks retained blocks directly, without a second key row per resource sample. Re-importing evidence that has aged out can accept it again; within the retained window, retries remain idempotent.
