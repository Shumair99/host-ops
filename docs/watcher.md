# Independent outside-in monitoring

Build `cmd/hostops-watcher` and run it on a separate machine. It needs only the site manifest and internet access, not Docker/SSH access. It checks DNS, TCP, validated TLS, HTTP status and latency, retaining results and incident counters in its own SQLite database.

```sh
hostops-watcher --config local/hostops.json --db local/watcher.db --interval 30s --failures 3 --recoveries 2
```

Three consecutive confirmed failures open an incident; two successful/warning cycles resolve it. Unknown observations break streaks without resolving active incidents. Threshold state survives process restart. Incident transitions are recorded atomically with threshold state. Running the watcher on the monitored VPS cannot detect total loss of that VPS.

`--api http://127.0.0.1:9090` can forward observations through a private SSH tunnel with `HOSTOPS_TOKEN`. If the agent is unavailable, the watcher's own history and outage detection continue. Set `HOSTOPS_WEBHOOK` to an HTTPS webhook to deliver incident transitions; tokenized webhook URLs stay outside Git/logs. Delivery status is recorded. Failed deliveries are currently recorded without retries. No destination is configured in the examples.
