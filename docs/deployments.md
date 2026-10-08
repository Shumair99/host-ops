# Observing existing deployments

Existing workflows keep responsibility for building, starting, checking and rolling back applications. HostOps imports their categorized result from a local JSON file or stdin; it does not execute deployments or expose a notification HTTP endpoint.

```json
{"site_id":"client-a","id":"run-123","status":"completed","commit":"81ca92f","image":"client-a:81ca92f","branch":"main","health_result":"healthy","duration_seconds":42}
```

Save the real workflow result to `local/deploy.json`, then run:

```powershell
.\bin\hostops.exe record deployment --config local/hostops.json --file local/deploy.json
# Or pipe JSON to stdin:
Get-Content -Raw local/deploy.json | .\bin\hostops.exe record deployment --config local/hostops.json --file -
```

The example uses a fictional `client-a`; use an ID registered in your manifest. Statuses: started/completed/failed. Repeated site/run/status notifications are idempotent. Only a categorized failure reason is accepted: build_failed, health_failed, script_failed or unknown. Imports accept one JSON value up to 256 KiB and reject unknown fields. Do not import raw deployment logs. Host identity, origin and import time are assigned by HostOps. A notification's health result is a reported result; the next collection independently verifies current runtime and endpoint health.

Image, runtime, restart/start time, proxy and health changes produce operational events. A changed image does not prove a successful CI run. `hostops deployments`, `hostops events` and `hostops logs <site>` expose these records and the correct runtime log stream. Log redaction covers common credential patterns but cannot guarantee removal of arbitrary application secrets; inspect before exporting.
