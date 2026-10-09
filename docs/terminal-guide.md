# Terminal dashboard and demonstration

## Start here in PowerShell

```powershell
cd path\to\host-ops
.\bin\hostops.exe dashboard --demo
```

CMD works too:

```bat
cd /d path\to\host-ops
bin\hostops.exe dashboard --demo
```

Windows Terminal with PowerShell is recommended for the colored block meters and box borders. CMD runs the same executable. The dashboard fills the terminal and restores the ordinary prompt when you quit. Minimum size is 60 columns and 16 rows; 120 by 36 gives the resource cards and full site table more room.

The demo header says `DEMO / FICTIONAL DATA`. Its CPU, disk, prices, sites, trends and logs are illustrative. It neither reads your VPS nor writes fictional health into your production database. Its history disappears when you exit; incident capsule export writes a local file only when you press `w`.

## A five-minute walkthrough

1. On Overview, read the colored CPU, Memory and Root disk meters. The fictional disk is 88% used to demonstrate a capacity warning. The property table budgets space for named health columns and always surfaces failed checks.
2. Press `s`. Use Down to select Example Bravo and Enter to open its property view. Scroll to see individual checks, doctor evidence and the Site Passport. Press `l` for fictional logs, then Esc to return to Sites.
3. Press `t`. The demo stops Bravo's fictional application and supplies the three confirmed failure observations needed to open an incident. DNS, TLS and proxy routing still pass; runtime, local HTTP and public HTTP fail. This is a scenario simulation, not a container stop.
4. Press `n`. Read the `[OPEN]` incident, symptoms, confidence and investigation. Press `w` if you want a redacted JSON capsule under `local/capsules/`. Review that file before sharing.
5. Press `t` again. The demo supplies two recovery observations. The same incident becomes `[RESOLVED]`; its original outage symptoms remain as historical evidence. Press `o` to see Bravo healthy again.
6. Press `d` for illustrative directory, property and Docker storage accounting. Right focuses the tree; Up/Down selects a directory, Enter opens it and Backspace returns to its parent. Parent/child totals overlap. `>=` means an incomplete lower bound. This view never cleans anything up.
7. Try `r` for readiness, `b` for recovery, `p` for capacity, or `x` for dependency impact. On Impact, `/` lets you enter a configured resource such as `caddy` or `alpha`.
8. Press `q` to return to your shell.

## Read the measurements and states

| Label | Meaning |
| --- | --- |
| `OK` / `PASS` | Check passed; green |
| `FAIL` / `CRITICAL` | Confirmed failure; red |
| `WARN` / `WARNING` | Warning; amber |
| `UNKNOWN` | Missing, unavailable or stale evidence |
| `N/A` / `SKIP` | Not applicable |
| `—` on a measurement | No measured value |
| `≥` on directory size | Partial measurement / lower bound |

The six checks are Runtime, Local HTTP, Proxy route, DNS, TLS and Public HTTP. Narrow terminals show fewer named columns; the inspector always retains all checks for each address. Meters show measured utilization from 0–100%. Sparklines scale to their own measured peak; Host history adds percent axes and actual timestamps. Blank chart columns indicate missing observations. Retained host history uses 15-second samples and is read locally, without additional VPS commands. The overall label is derived from checks; `HEALTHY` requires successful applicable observations.

Color is automatic in the interactive dashboard. Set `NO_COLOR` or use `--color never` to disable it; `--color always` also colors one-shot output. Status words remain legible without color. Instructions live here and under `?`; the short footer shows controls for the focused pane.

## Live use

Your existing private manifest is at `local/hostops.json`. For guided first-time setup, run:

```powershell
.\bin\hostops.exe start user@vps
```

To open the current connection directly:

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json
# Equivalent workspace shortcut:
.\scripts\dev.ps1
```

The header now says `LIVE / READ-ONLY`. The dashboard loads retained observations and starts one streaming SSH collector. CPU/RAM update every second using direct `/proc` reads, without spawning commands for those counters. Container stats and inventory/root disk update every 30 seconds. Container stats run immediately after the first inventory, then follow that schedule. Public HTTP uses `--interval` (default 30 seconds); DNS/TLS use five minutes. The first resource sample appears automatically, without pressing a key. Background refreshes keep the current evidence visible; only opening a new inspector shows its initial loading message. Without the optional VPS service, closing the dashboard stops live collection. With it enabled, a low-privilege systemd process keeps a rolling 30-minute host-metric history for the next connection.

The sites list comes from discovered proxy domains. Your existing reference property's JSON entry retains its display name and health preferences; other sites appear without registration. Overview and Sites show one row for an observed base domain and its `www` variant. That row reflects the worst health across both domains. Open the site inspector to see both addresses and their independent checks, including any `www`-only failure. Filtering by either address finds the same site. A standalone `www` domain and unrelated subdomains remain separate sites; no unobserved address is invented. Press `z` to inspect the individual source, route and workload facts. Wildcard/catch-all routes stay visible as coverage patterns. Public checks run from this computer, while the collector reads host/runtime configuration on the VPS.

For another VPS, `hostops start user@vps` guides the connection and asks whether to install the always-on host-metric service. SSH uses your existing identity and trusted host key in batch mode. The regular discovery collector runs from the remote user's private cache and exits with its SSH session. The optional history service is separate; a non-root SSH account authorizes installation with sudo, while a root SSH login already has that authority. It retains a rolling 30 minutes of CPU, memory, load, uptime and root-filesystem measurements. Use `hostops agent status|install|uninstall` to manage it. A missing or old Disk index is scanned in the background after discovery and cached for 30 minutes. Security, Preflight and runtime log views run bounded inspections on demand. You can switch views or quit during a storage scan. See [discovery support and limitations](discovery.md), [storage scope, overhead and controls](storage.md), and [service security boundaries](security.md).

The live root disk was measured at 88% used during verification. Open `d` to inspect directory sizes and Docker storage. Figures marked incomplete are lower bounds, not full totals. HostOps supplies evidence for a later cleanup decision; no keyboard shortcut deletes files or prunes Docker.

## Keyboard map

| Key | Action |
| --- | --- |
| Up / Down; `j` / `k` in menu | Preview the selected view immediately, keeping menu focus |
| Right / Enter in menu | Move focus into the displayed content |
| Left | Return focus to the Views menu |
| Tab / Shift+Tab | Switch focus between menu and content |
| View letter shown in menu | Jump directly to that view |
| Up / Down; `j` / `k` in content | Scroll; on Sites, select a property |
| PageUp / PageDown; Home / End | Move through the focused menu or evidence |
| Enter in Sites content | Inspect selected property |
| Enter / Right in Disk content | Open selected cached directory |
| Backspace in Disk content | Return to parent directory |
| `a` outside a site inspector | Open optional Tools; Up/Down selects and Enter opens a tool |
| `/` on Sites | Filter by name, ID or domain |
| `/` on Impact | Change resource being assessed |
| Space | Reload the current view; Disk requests a background scan |
| F5 | Request inventory refresh and public checks |
| `z` | Inventory, source coverage, routes and workloads |
| `l`, `a`, `b` from property | Logs, site readiness, site recovery |
| Esc | Return from an inspector to Sites or an optional tool to Tools; cancel input, clear site filter or focus menu |
| `w` on Incidents | Export newest listed incident capsule |
| `t` in demo | Toggle fictional outage/recovery |
| `?` | Help |
| `q` / Ctrl+C | Quit and restore terminal |

Main views: `o` Overview, `s` Sites, `i` Issues, `h` Host metrics, `z` Inventory, `d` Disk, `e` Timeline, `n` Incidents, `v` Health log, `a` Tools, `?` Help. The highlighted menu row identifies the view; the arrow by VIEWS indicates pane focus. Browsing switches the right pane immediately. Disk reads its retained directory index, so entering it or opening a directory launches no additional scan. Space requests another background inspection.

Tools contains Security (`u`), Capacity (`p`), Impact (`x`), Readiness (`r`), Recovery (`b`), Preflight (`f`), Changes (`g`), Deployments (`y`), External watcher (`m`), and Costs/setup (`c`). Their letters remain direct shortcuts. Logs, site readiness and recovery are also available inside each site inspector. Cold Security/Preflight inspections run only when explicitly opened, and retained results stay visible on subsequent visits. Costs show setup instructions until there is billing evidence, and omit fee/margin columns until client fees are configured. See [billing setup](economics.md).

## Optional background collector and plain commands

If you want history to continue while the dashboard is closed, run the headless collector in a separate terminal:

```powershell
.\bin\hostops-agent.exe --config local/hostops.json --db local/hostops.db --interval 30s
```

Then open the dashboard against the same database with `--no-collect`. That uses the agent's inventory and metrics and disables automatic Disk scans; inspection views can still run manual reads. `--no-collect --once --page disk` uses only retained storage evidence. Use one active collector per database to avoid duplicate samples. The collector stops with Ctrl+C and opens no management port.

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json --no-collect
.\bin\hostops.exe dashboard --config local/hostops.json --no-collect --once
.\bin\hostops.exe doctor --config local/hostops.json
.\bin\hostops.exe disk --config local/hostops.json
.\bin\hostops.exe readiness --config local/hostops.json
.\bin\hostops.exe preflight reboot --config local/hostops.json
.\bin\hostops.exe impact caddy --config local/hostops.json
.\bin\hostops.exe metrics --config local/hostops.json --json
```

`--once` prints one dashboard frame and exits, useful in redirected output without a terminal. Omit `--no-collect` to collect first. `--page disk` or another menu view chooses the initial page. Plain commands default to readable text; `--json` is for integrations. Capsule export is deliberately a JSON artifact. Query and capsule examples use the same local database unless `--db` overrides it.

## What happened to HOSTOPS_TOKEN?

`HOSTOPS_TOKEN` was a bearer password for the earlier web management API: the browser sent it to the local HTTP server to authorize requests. That entire web frontend and API have been removed. The CLI-only application does not read this environment variable, generate a token, prompt for one or listen on port 9090. Access now comes from your local user account, file permissions and existing SSH credentials.

Deployment, backup and restore evidence now enters through validated local JSON files or stdin with `hostops record`. A separate watcher keeps its own history and can export observations for import. See [deployments](deployments.md), [recovery](recovery.md), [watcher](watcher.md) and [security boundaries](security.md).
