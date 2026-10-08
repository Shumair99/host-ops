# Monitoring and disk investigation

The agent records health, HTTP latency, host CPU/memory/swap/load/uptime/root-disk metrics and Docker CPU/memory/network counters. Missing metrics are explicitly listed. Host CPU is calculated from two `/proc/stat` samples; it is a point-in-time observation, not a long-term average. Docker CPU may exceed 100% on multicore hosts.

Recent raw samples are retained for 90 days; the last observation for each host/site/kind is retained. Deployment, incident and recovery evidence is not covered by telemetry retention. There is no downsampling yet. Query endpoints are bounded to 1,000 records and return their observation times.

```sh
hostops metrics --config local/hostops.json
hostops metrics client-a --config local/hostops.json
hostops history client-a --config local/hostops.json --db local/hostops.db --since 24h
hostops disk --config local/hostops.json
```

`disk` reads directory sizes under application storage, Docker storage, logs and temporary files, and runs Docker's disk accounting. Directory totals overlap their child entries and must not be added together. Permission failures are shown rather than concealed. The command never prunes, deletes or changes retention. Accounting can identify candidates for investigation; it cannot determine that a volume or backup is safe to remove.
