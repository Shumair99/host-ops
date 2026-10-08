# Monitoring and disk investigation

The agent records health, HTTP latency, host CPU/memory/swap/load/uptime/root-disk metrics and Docker CPU/memory/network counters. Missing metrics are explicitly listed. Host CPU is calculated from two `/proc/stat` samples; it is a point-in-time observation, not a long-term average. Docker CPU may exceed 100% on multicore hosts.

CPU/RAM refresh every second, container stats every five seconds, and inventory/root disk every 30 seconds. Public HTTP defaults to 30 seconds; DNS/TLS use five minutes. Live state is kept current while resource history is written every 15 seconds. Resource and inventory history are retained for 90 days; the last observation for each host/site/kind is retained. Deployment, incident and recovery evidence is not covered by telemetry retention. Historical queries are bounded to 1,000 records and return their observation times. The dashboard runs the collector automatically; `hostops-agent` can continue collection while it is closed. See [discovery](discovery.md) for freshness and shared-backend accounting.

```sh
hostops metrics --config local/hostops.json
hostops metrics client-a --config local/hostops.json
hostops history client-a --config local/hostops.json --db local/hostops.db --since 24h
hostops disk --config local/hostops.json
```

`disk` scans the top-level directories of the root filesystem with a 45-second bound and runs Docker's disk accounting. Set `host.disk_roots` to narrow an investigation or include separately mounted filesystems; scans stay within each selected filesystem. Directory totals overlap their child entries and must not be added together. Permission/time/output limits are shown rather than concealed. The command never prunes, deletes or changes retention. Accounting identifies candidates for investigation; it cannot determine that a volume or backup is safe to remove.
