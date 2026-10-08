# Amadeus HostOps

A self-hosted operations platform that understands production websites across the whole chain: source, deployment, runtime, proxy, DNS and HTTPS.

HostOps observes existing Docker/Caddy infrastructure. It does not replace deployments, add itself to the website request path or automatically repair production.

Machine-specific manifests and inspection evidence belong in the ignored `local/` directory. Examples use fictional sites. Never add credentials, environment values, raw container inspections or backups to Git.

## Run locally

Requires Go 1.26+ and Node 22.12+ (or a current supported Node release).

```sh
go build -o bin/hostops ./cmd/hostops
go build -o bin/hostops-agent ./cmd/hostops-agent
cd web
npm ci
npm run build
cd ..
bin/hostops sites --config examples/sites/docker.json
```

Create your own ignored `local/hostops.json`, set a random `HOSTOPS_TOKEN` with at least 32 characters, then run:

```sh
bin/hostops-agent --config local/hostops.json --db local/hostops.db
```

Open http://127.0.0.1:9090 and enter the token. Use `host.ssh` to inspect a remote Linux host without installing HostOps there. See [security](docs/security.md), [diagnostics](docs/diagnostics.md), [monitoring](docs/monitoring.md) and the [isolated Docker demo](demo/README.md).
