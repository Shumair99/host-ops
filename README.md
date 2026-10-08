# Amadeus HostOps

A terminal operations desk that discovers websites on a Linux VPS. Connect with SSH, and HostOps combines runtime, proxy, service and socket observations into one inventory. The ASCII dashboard runs in PowerShell, CMD or a Linux terminal, with local SQLite history.

CPU and RAM refresh every second through direct Linux kernel-file reads. Container metrics, inventory and root disk refresh every 30 seconds. Sites come from observed proxy domains; you do not need to register them in JSON. Overview and Sites combine observed base/`www` domain pairs into one row; the inspector retains each domain's independent checks. Discovery coverage and unavailable sources are visible alongside the sites.

The dashboard uses your local account and existing SSH credentials. Collection leaves application and proxy configuration alone. A bundled Linux executable runs in the remote user's private cache for the duration of the SSH session; it exposes no management port. HostOps has no browser frontend or dashboard token.

## Try the offline demonstration

The Windows binaries are built under `bin/`. Open PowerShell:

```powershell
cd C:\path\to\host-ops
.\bin\hostops.exe dashboard --demo
```

In CMD, use `cd /d C:\path\to\host-ops` and `bin\hostops.exe dashboard --demo`. The demo needs no manifest, SSH, Docker, internet connection or token. It uses fictional sites in an isolated in-memory database. Press `s` for Sites, choose with arrows and press Enter. Press `t` to simulate an outage, `n` to inspect the incident, and `t` again to simulate recovery. Press `d` for illustrative disk accounting and `q` to exit.

## Connect a VPS

Build or obtain the full bundle, including `collectors/linux-amd64/hostops` and `collectors/linux-arm64/hostops` beside the main executable. With SSH access and a trusted host key already set up:

```powershell
.\bin\hostops.exe connect user@vps
.\bin\hostops.exe dashboard
```

`connect` verifies the connection, discovers supported routes, and saves ignored connection settings to `local/hostops.json`. Use `--identity C:/path/to/key` or `--port 2222` when needed; SSH aliases and existing SSH configuration also work. No private key contents are copied into settings. On this workspace, the existing connection is ready:

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json
```

Running `hostops` without a command also opens the dashboard. One persistent SSH collector supplies live metrics and inventory while public checks run independently from your computer. The public HTTP interval defaults to 30 seconds; DNS/TLS checks run every five minutes. `scripts/dev.ps1` opens the live dashboard, and `scripts/dev.ps1 -Demo` opens the offline demo.

Use a terminal at least 60 columns by 16 rows; 100 by 30 or larger shows more evidence. Up/Down selects a menu view; Right or Enter opens it. Left returns to the menu, and Tab switches focus between the two panes. In the content pane, Up/Down scrolls or selects a site, and Enter opens its inspector. Press `s` for sites, `z` for inventory/source coverage, `d` for disk investigation, F5 for fresh inventory/public checks, and `?` for help. Background refreshes preserve the displayed evidence. Missing and stale observations stay explicit.

Read the [terminal guide and walkthrough](docs/terminal-guide.md) for the full demo, live use and keyboard map. One-shot commands also produce readable text; add `--json` for scripts:

```powershell
.\bin\hostops.exe doctor --config local/hostops.json
.\bin\hostops.exe discover --config local/hostops.json
.\bin\hostops.exe disk --config local/hostops.json
.\bin\hostops.exe dashboard --demo --once
```

## Discovery support

Readers cover Docker, Podman, systemd/listeners, Caddy, Nginx, Traefik and explicit Apache virtual hosts. They all produce the same inventory; reconciliation links domains, routes and workloads using network-scoped aliases, addresses, published ports and sockets. Folder layouts and container names are observed rather than assumed.

Active configuration takes authority over declared configuration for the same proxy. Nginx/Apache dumps and Traefik labels are labelled declared; wildcard domains and catch-all rules are coverage patterns, not invented websites. Unsupported proxies, restricted permissions and opaque dynamic routing remain gaps. This release does not claim universal VPS coverage. See the [discovery architecture, support matrix and migration guide](docs/discovery.md).

## Build and development

Building requires Go 1.26 or newer. On Windows, `scripts/check.ps1` runs tests, vet, builds the three Windows executables and cross-compiles both Linux collector architectures. See [security boundaries](docs/security.md), and the optional [isolated Docker lab](demo/README.md).

Readiness, recovery, security/exposure, cost allocation, capacity, preflight, dependency impact, deployment records and incident capsules remain available through the terminal. Evidence enters through `hostops record`, described in the [deployment](docs/deployments.md), [recovery](docs/recovery.md) and [watcher](docs/watcher.md) guides. HostOps does not execute repairs, maintenance or restores.

Machine-specific manifests, history and inspection evidence belong in ignored `local/`. Keep credentials, private keys, environment values and raw runtime dumps out of Git.
