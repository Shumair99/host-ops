# Security boundaries

HostOps is a local terminal application. It opens no management HTTP listener and uses no dashboard bearer token. Access is governed by the operating-system user, local file permissions and existing SSH credentials. The optional headless collector and watcher also expose no management server.

Collection does not restart containers, reload shared Caddy, prune resources or execute user-defined shell hooks. Docker inspections select metadata; environment values and raw health log output are omitted. Command errors do not echo raw stderr. Docker access remains powerful: a read-only Docker socket mount does not restrict the operations available through Docker's API.

Remote inspections use SSH batch mode and strict host-key checking. Configuration is trusted operator input and defines the endpoint allowlist. Endpoint credentials and query strings are rejected. Private keys stay outside manifests and Git. Public HTTP/TLS probes use the executing machine's network; they do not establish independent outage detection when executed on the monitored VPS.

SQLite directories/files use restrictive POSIX modes. On Windows, protect `local/` with user-only ACLs because POSIX modes do not provide Windows ACL isolation. Use SQLite-aware backups for an active WAL database. Deployment, backup, restore and watcher JSON files require the same protection as their destination database. Imports validate host/site scoping, allowed fields, size and chronology; no arbitrary commands or raw logs are accepted.

Operational text is reduced to printable ASCII before terminal rendering, preventing embedded escape sequences from controlling the terminal. Logs and capsules redact common credential patterns, but arbitrary application secrets can remain. Review local artifacts before sharing. Capsules retain names, IDs and public domains; exporting a capsule does not publish it.

The previous web frontend, API, token setup and listening port have been removed. `HOSTOPS_TOKEN` is unused. Optional watcher notifications use the separate `HOSTOPS_WEBHOOK` HTTPS destination; none is configured by default. HostOps stays outside the website request path.
