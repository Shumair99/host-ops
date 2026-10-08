# Amadeus HostOps

A terminal operations desk that discovers websites on a Linux server. Connect with SSH, and HostOps combines runtime, proxy, service and socket observations into one inventory. The colored dashboard runs in PowerShell, CMD or a Linux terminal, with local SQLite history, resource meters and measured trends.

CPU and RAM refresh every second through direct Linux kernel-file reads. Container metrics, inventory and root disk refresh every 30 seconds. Sites come from observed proxy domains; you do not need to register them in JSON. Overview and Sites combine observed base/`www` domain pairs into one row; the inspector retains each domain's independent checks. Discovery coverage and unavailable sources are visible alongside the sites.

The dashboard uses your local account and existing SSH credentials. Collection leaves application and proxy configuration alone. A bundled Linux executable runs in the remote user's private cache for the duration of the SSH session; it exposes no management port. HostOps has no browser frontend or dashboard token.

## Try the offline demonstration

Build first with Go 1.26+: run `.\scripts\check.ps1` on Windows. On Linux/macOS run `go build -trimpath -o bin/hostops ./cmd/hostops`, then `bin/hostops dashboard --demo`. Remote connections also need the bundled Linux collectors. The Windows binaries are built under `bin/`. Open PowerShell:

```powershell
cd C:\path\to\host-ops
.\bin\hostops.exe dashboard --demo
```

In CMD, use `cd /d C:\path\to\host-ops` and `bin\hostops.exe dashboard --demo`. The demo needs no manifest, SSH, Docker, internet connection or token. It uses fictional sites in an isolated in-memory database. Press `s` for Sites, choose with arrows and press Enter. Press `t` to simulate an outage, `n` to inspect the incident, and `t` again to simulate recovery. Press `d` for illustrative disk accounting and `q` to exit.

## Connect a VPS

Build or obtain the full bundle, including the Linux discovery collectors and optional VPS history agents beside the main executable. With SSH key authentication and a trusted host key already set up, use the guided workflow:

```powershell
.\bin\hostops.exe start user@vps
```

The first run discovers supported routes, saves ignored settings to `local/hostops.json`, then asks whether to enable the optional rolling history service. It retains at most 30 minutes of host metrics and opens no public port. Installation uses `sudo` over the SSH terminal for a non-root account; a direct root SSH login already has the required authorization. HostOps does not save any password. Automated SSH password login is not supported: configure an identity through your SSH agent/config or pass `--identity C:/path/to/key`. Use `--port 2222` for a non-default SSH port.

The service runs on Linux amd64/arm64 with systemd. It uses a dedicated no-login `hostops` account, has no Linux capabilities, and grants the configured SSH account access only to the local telemetry socket and small history file. It does not scan site files or read file contents. Manage it later with `hostops agent status`, `hostops agent install`, or `hostops agent uninstall`.

The older two-step workflow remains available:

```powershell
.\bin\hostops.exe connect user@vps
.\bin\hostops.exe dashboard
```

Use `--identity C:/path/to/key` or `--port 2222` when needed; SSH aliases and existing SSH configuration also work. No private key contents are copied into settings. On this workspace, the existing connection is ready:

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json
```

Running `hostops` without a command also opens the dashboard. One persistent SSH collector supplies live metrics and inventory while public checks run independently from your computer. The public HTTP interval defaults to 30 seconds; DNS/TLS checks run every five minutes. `scripts/dev.ps1` opens the live dashboard, and `scripts/dev.ps1 -Demo` opens the offline demo.

Use a terminal at least 60 columns by 16 rows; 120 by 36 or larger shows more evidence. Up/Down in the menu previews each view immediately; Right or Enter focuses content. Left returns to the menu, and Tab switches pane focus. Sites combines a workload list with selected-property details; Enter opens the full inspector. Timeline and recent activity combine transitions, changes, deployments and incidents. A missing or old storage index is inspected automatically in the background and cached for 30 minutes. Disk's directory navigation uses that cache; Space requests another bounded scan. See [automatic storage inspection](docs/storage.md) for controls, ownership and measured overhead.

Costs are optional. `hostops billing` connects a Hetzner server or Hostinger subscription through API reads, or stores an explicit manual budget. Tokens stay in environment variables; estimates show their source and exclude expenses the reader cannot establish. Self-hosted machines can leave cost tracking disabled. See [billing setup](docs/economics.md).

Read the [terminal guide and walkthrough](docs/terminal-guide.md) for the full demo, live use and keyboard map. One-shot commands also produce readable text; add `--json` for scripts:

Windows Terminal with PowerShell is recommended. At 120 columns, Overview shows three resource cards and named checks within the available width; failures remain visible even when their columns do not fit. Smaller windows use compact panels. Press `?` for instructions. Interactive colors are automatic; `NO_COLOR` or `--color never` disables them. `--once` omits color unless `--color always` is requested.

```powershell
.\bin\hostops.exe doctor --config local/hostops.json
.\bin\hostops.exe discover --config local/hostops.json
.\bin\hostops.exe disk --config local/hostops.json
.\bin\hostops.exe dashboard --demo --once
```

## Discovery support

Readers cover Docker, Podman, systemd/listeners, Caddy, Nginx, Traefik and explicit Apache virtual hosts. They all produce the same inventory; reconciliation links domains, routes and workloads using network-scoped aliases, addresses, published ports and sockets. Alias ownership must be unique: a generic name shared by several containers stays unresolved. Folder layouts and container names are observed rather than assumed.

Active configuration takes authority over declared configuration for the same proxy. Nginx/Apache dumps and Traefik labels are labelled declared; wildcard domains and catch-all rules are coverage patterns, not invented websites. Unsupported proxies, restricted permissions and opaque dynamic routing remain gaps. This release does not claim universal VPS coverage. See the [discovery architecture, support matrix and migration guide](docs/discovery.md).

## Build and development

Building requires Go 1.26 or newer. On Windows, `scripts/check.ps1` runs tests, vet, builds the three Windows executables and cross-compiles both Linux collector architectures. See [security boundaries](docs/security.md), and the optional [isolated Docker lab](demo/README.md).

Readiness, recovery, security/exposure, cost allocation, capacity, preflight, dependency impact, deployment records and incident capsules remain available through the terminal. Evidence enters through `hostops record`, described in the [deployment](docs/deployments.md), [recovery](docs/recovery.md) and [watcher](docs/watcher.md) guides. HostOps does not execute repairs, maintenance or restores.

Machine-specific manifests, history and inspection evidence belong in ignored `local/`. Keep credentials, private keys, environment values and raw runtime dumps out of Git.

Local history has [configurable retention and a 256 MiB default database budget](docs/history.md). Windows history folders receive private ACLs. The project uses the [MIT license](LICENSE); see [security reporting](SECURITY.md). CI and bundle workflows are prepared for future publication; no public release is implied by a local build.
