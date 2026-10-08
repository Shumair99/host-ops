# Independent outside-in monitoring

Build `cmd/hostops-watcher` and run it on a separate machine. It needs only the site manifest and internet access, not Docker/SSH access. It checks DNS, TCP, validated TLS, HTTP status and latency, retaining results and incident counters in its own SQLite database.

```sh
hostops-watcher --config local/hostops.json --db local/watcher.db --interval 30s --failures 3 --recoveries 2
```

Three consecutive confirmed failures open an incident; two successful/warning cycles resolve it. Unknown observations break streaks without resolving active incidents. Threshold state survives process restart. Incident transitions are recorded atomically with threshold state. Running the watcher on the monitored VPS cannot detect total loss of that VPS.

The watcher keeps its own history even when the main dashboard/collector is unavailable. Add `--export local/observations.json` to save the latest batch for local CLI import. Transfer that file using your existing private file-transfer workflow, then import it within fifteen minutes:

```powershell
.\bin\hostops.exe record observations --config local/hostops.json --file local/observations.json
```

Imports require DNS, TCP, TLS and public HTTP layers, valid states, registered sites and fresh timestamps; duplicate batches do not advance incident thresholds. The External view shows imported observations. Export/import is optional and does not provide automatic cross-machine synchronization. There is no management HTTP API or bearer token.

To inspect the watcher's own local history, run `hostops dashboard --config local/hostops.json --db local/watcher.db --no-collect` on its machine. Automatic runtime collection is disabled; registered properties display outside-in health where local agent observations are absent. Host metrics and runtime checks remain unknown because the watcher does not collect them. Use the External and Incidents views for watcher evidence. Inspection views can still attempt manual runtime reads; they require configured SSH access.

Set `HOSTOPS_WEBHOOK` to an HTTPS webhook to deliver incident transitions if desired; tokenized webhook URLs stay outside Git/logs. Delivery status is recorded. Failed deliveries are currently recorded without retries. No destination is configured in the examples.
