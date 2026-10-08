# Terminal dashboard and demonstration

## Start here in PowerShell

```powershell
cd C:\path\to\host-ops
.\bin\hostops.exe dashboard --demo
```

CMD works too:

```bat
cd /d C:\path\to\host-ops
bin\hostops.exe dashboard --demo
```

PowerShell is convenient because it also runs the project's build and startup scripts. The dashboard itself is the same executable in both shells. It fills the terminal, accepts keyboard input, and restores the ordinary prompt when you quit. Resize to at least 60 columns and 16 rows; 100 by 30 is recommended.

The demo header says `DEMO / FICTIONAL DATA - NO NETWORK`. Its CPU, disk, prices, sites and logs are illustrative. It neither reads your VPS nor writes fictional health into your production database. Its history disappears when you exit; incident capsule export writes a local file only when you press `w`.

## A five-minute walkthrough

1. On Overview, read the CPU, RAM and DISK bars. The fictional disk is 88% used to demonstrate a capacity warning. Each property's six health symbols follow the layer order below.
2. Press `s`. Use Down to select Example Bravo and Enter to open its property view. Scroll to see individual checks, doctor evidence and the Site Passport. Press `l` for fictional logs, then Esc to return to Sites.
3. Press `t`. The demo stops Bravo's fictional application and supplies the three confirmed failure observations needed to open an incident. DNS, TLS and proxy routing still pass; runtime, local HTTP and public HTTP fail. This is a scenario simulation, not a container stop.
4. Press `n`. Read the `[OPEN]` incident, symptoms, confidence and investigation. Press `w` if you want a redacted JSON capsule under `local/capsules/`. Review that file before sharing.
5. Press `t` again. The demo supplies two recovery observations. The same incident becomes `[RESOLVED]`; its original outage symptoms remain as historical evidence. Press `o` to see Bravo healthy again.
6. Press `d` for illustrative directory and Docker storage accounting. Parent/child totals overlap. `>=` means an incomplete lower bound. This view never cleans anything up.
7. Try `r` for readiness, `b` for recovery, `p` for capacity, or `x` for dependency impact. On Impact, `/` lets you enter a configured resource such as `caddy` or `alpha`.
8. Press `q` to return to your shell.

## Read the symbols

| Symbol | Meaning |
| --- | --- |
| `[+]` | Check passed |
| `[!]` | Confirmed failure |
| `[~]` | Warning |
| `[?]` | Missing, unavailable or unknown evidence |
| `[-]` | Not applicable |

The six columns are **R**untime, **L**ocal HTTP, **P**roxy route, **D**NS, **T**LS and public **H**TTP. Narrow terminals use a compact six-character strip with the same order. Unknown CPU/RAM/disk uses question marks rather than a fabricated zero. The overall label is derived from the checks; `HEALTHY` requires successful applicable observations.

## Live use

Your existing private manifest is at `local/hostops.json`. Start:

```powershell
.\bin\hostops.exe dashboard --config local/hostops.json
# Equivalent workspace shortcut:
.\scripts\dev.ps1
```

The header now says `LIVE / READ-ONLY`. The dashboard loads retained observations and starts one streaming SSH collector. CPU/RAM update every second using direct `/proc` reads, without spawning commands for those counters. Container stats and inventory/root disk update every 30 seconds. Container stats run immediately after the first inventory, then follow that schedule. Public HTTP uses `--interval` (default 30 seconds); DNS/TLS use five minutes. The first resource sample appears automatically, without pressing a key. Background refreshes keep the current evidence visible; only opening a new inspector shows its initial loading message. Closing the dashboard cancels its reads and stops its collector.

The sites list comes from discovered proxy domains. Your existing reference property's JSON entry retains its display name and health preferences; other sites appear without registration. Overview and Sites show one row for an observed base domain and its `www` variant. That row reflects the worst health across both domains. Open the site inspector to see both addresses and their independent checks, including any `www`-only failure. Filtering by either address finds the same site. A standalone `www` domain and unrelated subdomains remain separate sites; no unobserved address is invented. Press `z` to inspect the individual source, route and workload facts. Wildcard/catch-all routes stay visible as coverage patterns. Public checks run from this computer, while the collector reads host/runtime configuration on the VPS.

For another VPS, run `hostops connect user@vps` first, optionally with `--identity FILE` or `--port 2222`. It writes connection settings after a successful inspection. SSH uses your existing identity and trusted host key in batch mode. The bundled Linux collector runs from the remote user's private cache; it installs no service and needs no dashboard token. Disk, Security, Preflight and runtime log views run bounded inspections on demand. Disk scans can take longer than history reads; switch views or quit while they run. See [discovery support and limitations](discovery.md).

The live root disk was measured at 88% used during verification. Open `d` to inspect directory sizes and Docker storage. Figures marked incomplete are lower bounds, not full totals. HostOps supplies evidence for a later cleanup decision; no keyboard shortcut deletes files or prunes Docker.

## Keyboard map

| Key | Action |
| --- | --- |
| Up / Down; `j` / `k` in menu | Select a view without opening it |
| Right / Enter in menu | Open the selected view and focus its content |
| Left | Return focus to the Views menu |
| Tab / Shift+Tab | Switch focus between menu and content |
| View letter shown in menu | Jump directly to that view |
| Up / Down; `j` / `k` in content | Scroll; on Sites, select a property |
| PageUp / PageDown; Home / End | Move through the focused menu or evidence |
| Enter in Sites content | Inspect selected property |
| `/` on Sites | Filter by name, ID or domain |
| `/` on Impact | Change resource being assessed |
| Space | Reload the current view |
| F5 | Request inventory refresh and public checks |
| `z` | Inventory, source coverage, routes and workloads |
| `l`, `a`, `b` from property | Logs, site readiness, site recovery |
| Esc | Return from an inspector to Sites; cancel input, clear site filter or focus menu |
| `w` on Incidents | Export newest listed incident capsule |
| `t` in demo | Toggle fictional outage/recovery |
| `?` | Help |
| `q` / Ctrl+C | Quit and restore terminal |

Views: `o` Overview, `s` Sites, `i` Issues, `h` Host metrics, `z` Inventory, `d` Disk, `r` Readiness, `e` Events, `g` Changes, `y` Deployments, `n` Incidents, `v` History, `m` External watcher checks, `u` Security, `c` Costs, `p` Capacity, `b` Recovery, `f` reboot Preflight, `x` Impact. The menu scrolls to keep its selected view visible. `>` marks the menu cursor, `*` marks the open view, and `[FOCUS]` identifies the pane receiving navigation keys. View-letter shortcuts open their content directly.

## Optional background collector and plain commands

If you want history to continue while the dashboard is closed, run the headless collector in a separate terminal:

```powershell
.\bin\hostops-agent.exe --config local/hostops.json --db local/hostops.db --interval 30s
```

Then open the dashboard against the same database with `--no-collect`. That uses the agent's inventory and metrics; inspection views can still run manual reads. Use one active collector per database to avoid duplicate samples. The collector stops with Ctrl+C and opens no management port.

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
