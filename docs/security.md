# Security boundaries

The management API only binds to an explicit loopback IP and requires a bearer token of at least 32 characters. Set `HOSTOPS_TOKEN` outside Git. Use SSH tunnelling to reach a remote agent. No Docker socket, admin API or management dashboard should be exposed to the public internet.

Collection does not restart containers, reload Caddy, prune resources or run user-defined shell hooks. Docker inspections project selected metadata; environment values and raw health log output are omitted. Errors do not echo command stderr. Docker read-only socket mounts do not make Docker's API read-only: Docker access still grants host-level authority.

SQLite directories/files are created with restrictive POSIX modes. On Windows, protect `local/` with user-only ACLs; POSIX mode bits do not establish Windows ACL isolation. SQLite backups need the same protection. Use SQLite-aware backups, not an uncoordinated copy of an active WAL database.

Configuration is trusted operator input. Endpoints cannot include credentials or query strings. Private host identity and keys stay out of the repository. Health/API responses contain only operational metadata. Logs, when requested later, require additional redaction and must not be assumed secret-free.
