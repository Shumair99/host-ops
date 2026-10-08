# Amadeus HostOps

A self-hosted operations platform that understands production websites across the whole chain: source, deployment, runtime, proxy, DNS and HTTPS.

HostOps observes existing Docker/Caddy infrastructure. It does not replace deployments, add itself to the website request path or automatically repair production.

Machine-specific manifests and inspection evidence belong in the ignored `local/` directory. Examples use fictional sites. Never add credentials, environment values, raw container inspections or backups to Git.
