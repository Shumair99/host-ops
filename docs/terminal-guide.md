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

The header now says `LIVE / READ-ONLY`. The dashboard loads retained observations and immediately starts fresh checks; unavailable or stale evidence remains visible. It collects again every 60 seconds, or at `--interval 30s`. Collection cycles do not overlap. Closing the terminal dashboard cancels its reads and stops its collector.

The current local manifest registers one reference property. Add the other sites to that manifest to monitor their full paths individually; discovering a container does not automatically register a production property. Use the templates under `examples/sites/`. Public checks run from this computer, while configured SSH performs runtime/host checks on the VPS.

`host.ssh` uses your existing SSH identity and trusted host key, in batch mode with strict host-key checking. HostOps does not request a token or install an agent on the VPS. Disk, Security, Preflight and runtime log views run bounded read-only inspections when opened or reloaded. Disk scans can take longer than ordinary history reads; switch views or quit while they run.

The live root disk was measured at 88% used during verification. Open `d` to inspect directory sizes and Docker storage. Figures marked incomplete are lower bounds, not full totals. HostOps supplies evidence for a later cleanup decision; no keyboard shortcut deletes files or prunes Docker.

## Keyboard map

| Key | Action |
| --- | --- |
| Tab / Shift+Tab; Left / Right | Next / previous view |
| View letter shown in menu | Jump directly to that view |
| Up / Down; `j` / `k` | Scroll; on Sites, select a property |
| PageUp / PageDown; Home / End | Scroll longer evidence |
| Enter on Sites | Inspect selected property |
| `/` on Sites | Filter by name, ID or domain |
| `/` on Impact | Change resource being assessed |
| Space | Reload the current view |
| F5 | Collect fresh live observations |
| `l`, `a`, `b` from property | Logs, site readiness, site recovery |
| Esc | Return from property/logs; cancel input or clear site filter |
| `w` on Incidents | Export newest listed incident capsule |
| `t` in demo | Toggle fictional outage/recovery |
| `?` | Help |
| `q` / Ctrl+C | Quit and restore terminal |

Views: `o` Overview, `s` Sites, `i` Issues, `h` Host metrics, `d` Disk, `r` Readiness, `e` Events, `g` Changes, `y` Deployments, `n` Incidents, `v` History, `m` External watcher checks, `u` Security, `c` Costs, `p` Capacity, `b` Recovery, `f` reboot Preflight, `x` Impact. The menu scrolls to keep the active view visible.

## Optional background collector and plain commands

If you want history to continue while the dashboard is closed, run the headless collector in a separate terminal:

```powershell
.\bin\hostops-agent.exe --config local/hostops.json --db local/hostops.db --interval 60s
```

Then open the dashboard against the same database with `--no-collect`. That disables automatic collection cycles; inspection views can still run manual reads. Use one active collector per database to avoid duplicate samples. The collector stops with Ctrl+C and opens no management port.

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
