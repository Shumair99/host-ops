# Observing existing deployments

Existing workflows can optionally POST to the private agent through an SSH tunnel. They keep responsibility for building, starting, checking and rolling back applications. HostOps records notifications; it does not execute deployments.

```json
{"site_id":"client-a","id":"run-123","status":"completed","commit":"81ca92f","image":"client-a:81ca92f","branch":"main","health_result":"healthy","duration_seconds":42}
```

Use `Authorization: Bearer $HOSTOPS_TOKEN`. Statuses: started/completed/failed. Repeated site/run/status notifications are idempotent. Only a categorized failure reason is accepted: build_failed, health_failed, script_failed or unknown. Do not transmit raw deployment logs. A notification's health result is a reported result; the next agent sample independently verifies current runtime and endpoint health.

Image, runtime, restart/start time, proxy and health changes produce operational events. A changed image does not prove a successful CI run. `hostops deployments`, `hostops events` and `hostops logs <site>` expose these records and the correct runtime log stream. Log redaction covers common credential patterns but cannot guarantee removal of arbitrary application secrets; inspect before exporting.
