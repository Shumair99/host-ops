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

Set `HOSTOPS_WEBHOOK` to an HTTPS webhook to deliver incident transitions if desired; tokenized webhook URLs stay outside Git/logs. A transport failure, HTTP 408/429 or 5xx gets one retry after 250 ms. Each attempt has a five-second timeout, with an eleven-second total limit. Redirects and other non-success responses are not followed/retried. A stable Idempotency-Key identifies the incident and open/resolved transition across attempts; the receiver must honor it to prevent duplicate effects after an uncertain response. Final delivered/failed status, transition and a sanitized failure reason are saved as alert evidence. There is no persistent delivery queue or unlimited replay. No destination is configured in the examples. The watcher's configurable finite history and size guard are described in [history](history.md).
