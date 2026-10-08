# Amadeus HostOps

A terminal-only operations desk for production websites. The interactive dashboard uses ASCII borders, health symbols and resource bars inside PowerShell, CMD or a Linux terminal. It observes runtime, local HTTP, Caddy, DNS, TLS and public HTTP, and keeps history in SQLite.

There is no browser frontend, management HTTP server, listening dashboard port or access token. Your existing deployments keep responsibility for changing production. HostOps inspections are read-only.

## Try the offline demonstration

The Windows binaries are built under `bin/`. Open PowerShell:

```powershell
cd C:\path\to\host-ops
.\bin\hostops.exe dashboard --demo
```

In CMD, use `cd /d C:\path\to\host-ops` and `bin\hostops.exe dashboard --demo`. The demo needs no manifest, SSH, Docker, internet connection or token. It uses fictional sites in an isolated in-memory database. Press `s` for Sites, choose with arrows and press Enter. Press `t` to simulate an outage, `n` to inspect the incident, and `t` again to simulate recovery. Press `d` for illustrative disk accounting and `q` to exit.

## Inspect your infrastructure

With the ignored `local/hostops.json` manifest configured:

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json
```

Running `hostops` without a command also opens the dashboard. It collects automatically every 60 seconds using your existing SSH identity and known_hosts; no installation on the VPS is required. The standalone collector is optional. `scripts/dev.ps1` opens the same live dashboard, and `scripts/dev.ps1 -Demo` opens the offline demo.

Use a terminal at least 60 columns by 16 rows; 100 by 30 or larger shows more evidence. Tab changes views, arrows scroll, Space reloads a view, F5 collects fresh observations, and `?` shows help. Missing and stale observations stay explicit.

Read the [terminal guide and walkthrough](docs/terminal-guide.md) for the full demo, live use and keyboard map. One-shot commands also produce readable text; add `--json` for scripts:

```powershell
.\bin\hostops.exe doctor --config local/hostops.json
.\bin\hostops.exe disk --config local/hostops.json
.\bin\hostops.exe dashboard --demo --once
```

## Build and development

Building requires Go 1.26 or newer. Node/npm are not required. On Windows, `scripts/check.ps1` runs tests, vet and builds the three executables. See [security boundaries](docs/security.md), and the optional [isolated Docker lab](demo/README.md).

Readiness, recovery, security/exposure, cost allocation, capacity, preflight, dependency impact, deployment records and incident capsules remain available through the terminal. Evidence enters through `hostops record`, described in the [deployment](docs/deployments.md), [recovery](docs/recovery.md) and [watcher](docs/watcher.md) guides. HostOps does not execute repairs, maintenance or restores.

Machine-specific manifests, history and inspection evidence belong in ignored `local/`. Keep credentials, private keys, environment values and raw runtime dumps out of Git.
