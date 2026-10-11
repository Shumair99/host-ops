# Terminal dashboard and demonstration

## Start here

Install the complete bundle using its installer, then open a new terminal. See [installation and onboarding](install.md).

```powershell
hostops start
# Or try fictional data without connecting:
hostops start --demo
```

CMD works too:

```bat
hostops start
```

Windows Terminal with PowerShell is recommended for the colored block meters and box borders. CMD runs the same executable. The dashboard fills the terminal and restores the ordinary prompt when you quit. Minimum size is 60 columns and 16 rows; 120 by 36 or larger gives the paired visual workspaces more room.

The demo header says `DEMO / FICTIONAL DATA`. Its CPU, disk, prices, sites, trends and logs are illustrative. It neither reads your VPS nor writes fictional health into your production database. Its history disappears when you exit; incident capsule export writes a local file only when you press `w`.

## A five-minute walkthrough

1. On Overview, compare the host trend canvas, six-property health matrix, public response times and workload memory rankings. The fictional disk is 88% used to demonstrate a capacity warning. Host plots share a 0–100% scale; blank time columns mean missing samples.
2. Press `s`. Use Down to select Example Bravo and Enter to open its property view. Scroll to see individual checks, doctor evidence and the Site Passport. Press `l` for fictional logs, then Esc to return to Sites.
3. Press `t`. The demo stops Bravo's fictional application and supplies the three confirmed failure observations needed to open an incident. DNS, TLS and proxy routing still pass; runtime, local HTTP and public HTTP fail. This is a scenario simulation, not a container stop.
4. Press `n`. Choose an incident with Up/Down and read its status, symptoms and investigation. Press `w` to export the selected redacted JSON capsule under `local/capsules/`. Review that file before sharing.
5. Press `t` again. The demo supplies two recovery observations. The same incident becomes `[RESOLVED]`; its original outage symptoms remain as historical evidence. Press `o` to see Bravo healthy again.
6. Press `d` for illustrative directory, property and Docker storage accounting. Right focuses the tree; Up/Down selects a directory, Enter opens it and Backspace returns to its parent. Parent/child totals overlap. `>=` means an incomplete lower bound. This view never cleans anything up.
7. Try `h` for network, load, memory and pressure instruments. Try `r` for readiness, `b` for recovery, `p` for capacity, or `x` for a dependency tree. Up/Down selects the property on readiness/recovery; `1` expands passing readiness/security evidence. On Impact, `/` selects a configured resource such as `caddy` or `alpha`.
8. Press `q` to return to your shell.

## Read the measurements and states

| Label | Meaning |
| --- | --- |
| `OK` / `PASS` | Check passed; green |
| `FAIL` / `CRITICAL` | Confirmed failure; red |
| `WARN` / `WARNING` | Warning; amber |
| `UNKNOWN` | Missing, unavailable or stale evidence |
| `PROPAGATING.` → `..` → `...` | Initial dashboard data is still loading; routine refreshes do not restart it |
| `N/A` / `SKIP` | Not applicable |
| `—` on a measurement | No measured value |
| `≥` on directory size | Partial measurement / lower bound |

**Export for AI:** navigate to this entry at the bottom of the view menu and press `Enter` to review, then `Enter` again to confirm saving all monitoring data and the AI review prompt as Markdown. `Esc` cancels; `P` changes the folder. Uppercase `E` also opens the page. The file stays local until you share it. See [AI export contents and limits](ai-export.md).

The six checks are Runtime, Local HTTP, Proxy route, DNS, TLS and Public HTTP. Narrow terminals abbreviate the column names; the inspector always retains all checks for each address. Meters show measured utilization from 0–100%. Sparklines scale to their own measured peak; Host history adds percent axes and actual timestamps. Blank chart columns indicate missing observations; half-height cells represent small nonzero readings and a thin baseline represents measured zero. Retained host history uses 15-second samples and is read locally, without additional VPS commands. Numeric readouts remain live, while chart bars, scales and time windows change only when saved samples arrive. The right axis label gives the latest saved sample's time. The overall label is derived from checks; `HEALTHY` requires successful applicable observations.

Color is automatic in the interactive dashboard. Set `NO_COLOR` or use `--color never` to disable it; `--color always` also colors one-shot output. Status words remain legible without color. Instructions live here and under `?`; the short footer shows controls for the focused pane.

The header animates `PROPAGATING` while the initial connection, inventory, host/workload samples or public checks are arriving. Once that first load settles, navigation, storage scans, refreshes and reconnects do not restart the animation. Missing or unavailable evidence also completes loading; it does not leave the dashboard perpetually propagating. Completed evidence stays visible during refreshes.

Colored charts and meters use ANSI background fills for full cells, with Unicode half blocks for fractional heights. Borders use Unicode box drawing, not ASCII hyphens. ANSI controls styling; it is not a separate character alphabet. Monochrome charts retain block glyphs and a thin baseline for measured zero, while missing observations remain blank. Windows Terminal with Cascadia Mono or Consolas gives continuous cell rendering; legacy console font/line-spacing behavior remains outside the app's control.

The health matrix keeps one row per property. The Operations feed beneath the charts pins current notices above recent activity, even when no deployments or other changes occur. Notices clear when the current evidence recovers and identical alias notices are consolidated. On an unhealthy property, the passport and Issues show the main supported finding. `Observed` describes measured facts, `Likely` describes an inference, and `Unconfirmed` leaves the cause open. The property inspector exposes supporting checks and next steps. Supplemental registry evidence can identify holds or elapsed registry expiration dates, but never assumes that DNS failure means lost domain ownership. See [diagnostic evidence and limits](diagnostics.md).

## Live use

For guided first-time setup and subsequent sessions, run:

```powershell
hostops start
```

Settings are saved privately in your operating system's user configuration folder. Existing project-local settings and explicit `--config` paths remain supported. To open the current connection directly without onboarding:

```powershell
hostops dashboard
```

The header now says `LIVE / READ-ONLY`. The dashboard loads retained observations and starts one streaming SSH collector. CPU/RAM update every second using direct `/proc` reads, without spawning commands for those counters. Container stats and inventory/root disk update every 30 seconds. Container stats run immediately after the first inventory, then follow that schedule. Public HTTP uses `--interval` (default 30 seconds); DNS/TLS use five minutes. The first resource sample appears automatically, without pressing a key. Background refreshes keep the current evidence visible; only opening a new inspector shows its initial loading message. Without the optional VPS service, closing the dashboard stops live collection. With it enabled, a low-privilege systemd process keeps a rolling 30-minute host-metric history for the next connection.

The sites list comes from discovered proxy domains. Your existing reference property's JSON entry retains its display name and health preferences; other sites appear without registration. Overview and Sites group observed subdomains under their outermost observed parent within the same registrable domain. `+2` beside a property means two related domains. The row reflects the worst health across its domains, while the inspector preserves each address's own checks. Filtering by any related address finds the parent; Ctrl+P can open a subdomain directly with its own runtime, logs and assessment shortcuts. Separate workloads retain separate CPU/memory values in resource rankings and property details; references to the same workload count once. CPU/memory sorting uses the busiest member, and public-response rankings show the slowest measured endpoint per property. A missing parent is never invented, and unrelated tenants under a shared hosting suffix remain separate. Press `z` to inspect the individual source, route and workload facts. Wildcard/catch-all routes stay visible as coverage patterns. Public checks run from this computer, while the collector reads host/runtime configuration on the VPS.

For another VPS, use a separate settings file: `hostops start user@vps --config path/to/another-vps.json`. Onboarding verifies your existing SSH key and lets OpenSSH confirm a new host fingerprint; collection then uses batch mode with strict host verification. The regular discovery collector runs from the remote user's private cache and exits with its SSH session. The optional history service is separate; a non-root SSH account authorizes installation with sudo, while a root SSH login already has that authority. It retains a rolling 30 minutes of CPU, memory, load, uptime and root-filesystem measurements. Use `hostops agent status|install|uninstall` to manage it. A missing or old Disk index is scanned in the background after discovery and cached for 30 minutes. Security, Preflight and runtime log views run bounded inspections on demand. You can switch views or quit during a storage scan. See [discovery support and limitations](discovery.md), [storage scope, overhead and controls](storage.md), and [service security boundaries](security.md).

Open `d` to inspect measured directory sizes and Docker storage. Figures marked incomplete are lower bounds, not full totals. HostOps supplies evidence for a later cleanup decision; no keyboard shortcut deletes files or prunes Docker.

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
| `Ctrl+P` | Search and open any view or property; Esc returns to the current workspace |
| `2` on Sites | Cycle name, health, CPU, memory and latency sorting |
| Up/Down on Readiness / Recovery / Incidents | Select a property or incident; PageUp/PageDown scroll details |
| `1` on Readiness / Security | Show or hide passing rules / findings |
| `w` on Incidents | Export the selected incident capsule |
| `t` in demo | Toggle fictional outage/recovery |
| `?` | Help |
| `q` / Ctrl+C | Quit and restore terminal |

Main views: `o` Overview, `s` Sites, `i` Issues, `h` Host metrics, `z` Inventory, `d` Disk, `e` Timeline, `n` Incidents, `v` Health log, `a` Tools, `?` Help. The highlighted menu row identifies the view; the arrow by VIEWS indicates pane focus. Browsing switches the right pane immediately. Disk reads its retained directory index, so entering it or opening a directory launches no additional scan. Space requests another background inspection.

Tools contains Security (`u`), Capacity (`p`), Impact (`x`), Readiness (`r`), Recovery (`b`), Preflight (`f`), Changes (`g`), Deployments (`y`), External watcher (`m`), and Costs/setup (`c`). Their letters remain direct shortcuts. Logs, site readiness and recovery are also available inside each site inspector. Cold Security/Preflight inspections run only when explicitly opened, and retained results stay visible on subsequent visits. Costs offers an editable billing form directly on the page, and omits fee/margin columns until client fees are configured. See [billing setup](economics.md).

Arrow keys wrap at both ends of selectable lists: Views, Tools, Sites, directories, readiness/recovery properties, incidents and jump results. Home/End jumps to their edges. Esc goes back one level, including from site logs/assessments to that site's inspector. Mouse-wheel events scroll the pane under the pointer; over Views, Tools, Sites and setup choices they move the selection. Document scrolling stops at the top/bottom. The sidebar and visible setup actions also accept clicks.

Press **S** on Recovery to import an existing backup/restore JSON report or request another discovery scan. Detected backup artifacts remain distinct from successful job reports and verified restores. Deployment and External pages offer their report imports with **Enter / S**. These are local evidence imports; they do not perform a backup, restore or deployment.

See [Understanding the operations tools](operations-tools.md) for the meaning of evidence scores, Impact, recovery targets and Security findings. Security counts each shared app container once, uses property names, and treats missing hardening metadata as unknown. Readiness percentages use only applicable checks. Base/www endpoints stay adjacent in domain tables and maps.

## Optional background collector and plain commands

If you want history to continue while the dashboard is closed, run the headless collector in a separate terminal:

```powershell
.\bin\hostops-agent.exe --config local/hostops.json --interval 30s
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
