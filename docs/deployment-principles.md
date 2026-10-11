# Deployment assessment principles

HostOps discovers the deployment it observes and evaluates it against the same operational criteria on every host. It does not require a particular domain, network name, container name, numeric user ID or website framework.

## Facts and recommendations

* Effective process identities determine current runtime permissions. A configured starting user alone cannot establish which user serves requests.
* Every observed container-root process retains the non-root hardening warning. Recognized supervisor/worker roles explain the remaining privileges without removing the readiness criterion.
* Complete observations of non-root processes pass that criterion regardless of framework or configured starting user. Incomplete observations remain unknown.
* A warning records a hardening opportunity. It does not assert an exploit, intrusion or broken website. A pass establishes only the inspected fact.
* Runtime-specific checks are skipped when the runtime does not exist, such as a proxy-only redirect. A service that exists keeps its applicable requirements.
* Confirmed shared addresses can share an application assessment; independent endpoint failures remain visible. Separate applications retain their own evidence.
* Missing dependency or recovery evidence is explicit. An artifact, shared network or familiar setup cannot replace verified job outcomes or observed connections.

## A consistent permission policy

Each service should run with only the permissions it needs, under its own appropriate user. A common safety policy does not require a common server implementation or shared account.

Nginx can retain its supervisor/worker architecture while both roles run without root. Node and Java applications can run directly under a non-root user. Caddy can run under its own non-root user with the limited permission needed to bind its public ports and access its configuration/certificate storage. PostgreSQL uses its own database process user and writable data directory; its data ownership and internal database accounts need separate handling.

Permission changes belong in the application's image and deployment configuration. Required ports, file ownership, writable directories, startup and real request handling must be verified before replacing a working deployment. HostOps criteria do not change to improve a particular host's score.

Primary references: [Docker security](https://docs.docker.com/engine/security/), [Nginx unprivileged image](https://github.com/nginx/docker-nginx-unprivileged), [Caddy permissions](https://caddyserver.com/docs/quick-starts/caddyfile), [PostgreSQL image users and data ownership](https://github.com/docker-library/docs/blob/master/postgres/README.md).
