# Export for AI

Navigate to **Export for AI** under **EXPORT** at the bottom of the view menu and press **Enter** to review the export. A confirmation screen shows what will be saved and its destination; press **Enter** again to save, or **Esc** to cancel. Browsing or opening the view never writes a file. Uppercase **E** also opens the export page, where Enter opens the same confirmation. **P** changes the destination folder; paste or type a path, then Enter saves the folder choice and Esc cancels editing. The default folder is `HostOps/exports` inside your user home directory. The saved file has a unique host-and-timestamp name; a later export never replaces an earlier one.

Attach the resulting file to an AI conversation. Its opening prompt asks for a plain-language VPS review organized by affected domains and shared services, with references back to the relevant dashboard views, prioritized changes, concrete configuration edits, rollback steps and verification. It asks the AI to improve the server and deployment process, retain HostOps criteria, distinguish confirmed problems from unknown evidence and optional hardening, and resolve justified warnings through actual configuration improvements. The export makes no AI request and performs no server changes.

## Contents

All discovered addresses and workloads are included, independently of the selected page, property, filter or passing-rule toggle. The file contains Overview, Sites, Issues, Host metrics, Inventory, Disk usage, Timeline, Incidents, Health log, Security, Capacity, Impact, Readiness, Recovery, Preflight, Changes, Deployments, External and Costs / setup. Typed evidence retains domain/site identities, health checks, diagnoses, confidence, runtime permissions, image/revision references, routes, topology, storage metadata and applicable assessment rules. Passing evidence is retained as well as warnings, failures and unknowns.

This is a consolidated evidence catalog, not pasted screens or separate dumps of each view. A domain identity index resolves site IDs. Facts are grouped into domains, runtimes, dependencies/storage, health, checks, measurements, records and assessment context. Exactly identical objects appear once; references preserve every view's membership and relationships, with source provenance and domain subjects. Different timestamps, confidence levels or results remain separate. Compact JSON retains field names and types without repeated indentation. The schema explains references, unavailable sources and the distinction between missing, empty, null and measured zero. Consolidation does not reassess or reinterpret HostOps results.

Stored observations are loaded at capture start. Assessments use the same bounded read-only readers as their dashboard views, including Security and Preflight even if they have not been opened. Capture start/end and per-section times are shown; existing observations keep their own timestamps. This is a capture window, not an atomic simultaneous probe of the whole VPS. The active dashboard continues collecting and navigating while the export runs. Quitting cancels unfinished work.

Host and per-address metric histories preserve every captured sample, up to 120 saved samples in the last 30 minutes, in CSV matrices with metric names written once. Identical alias histories share a matrix while their address identities remain in explicit scope bindings. Each sample retains its timestamp, missing metrics and unavailable reasons. Statistics include sample count, minimum, maximum, p95 and latest value for every observed metric; they supplement the full histories. Current metrics remain intact. Network rates use the same counter-reset and interface checks as the graphs. Historical feeds follow the dashboard's latest 100 retained records in 24 hours, and incidents include their persisted current state. These limits are stated in the file; this is not an unlimited database dump. Directory information comes from the retained index; exporting does not start another filesystem scan. An unavailable assessment is explicitly marked and does not discard the other sections. An export exceeding 10 MiB fails visibly instead of silently truncating evidence.

## Local file and privacy

The file stays local until the user shares it. Environment values, SSH credentials, raw configuration files and runtime log bodies are excluded. The writer removes known secret fields/patterns and URL credentials, query strings and fragments, including those in imported evidence. Structured JSON keeps arbitrary labels/report text inside data blocks; the prompt tells the AI to treat them as untrusted evidence. Redaction cannot promise to recognize arbitrary secrets hidden in free text. Domains, addresses, paths and billing figures remain useful but private infrastructure information, so review the file before sharing it. New files use owner-only permissions on Unix or a protected current-user/SYSTEM ACL on Windows. Failed writes remove the newly created partial file; existing exports are preserved.

## Command line

```powershell
hostops export-ai --config hostops.json --output review.md
hostops export-ai --demo --output demo-review.md
```

Use `--db FILE` for an explicit history database and `--json` for saved-file metadata. With no output option, the command uses the same home-directory export folder. Demo export uses an isolated in-memory history and makes no network calls. Export reads retained inventory/health and runs on-demand assessments; open the live dashboard first if current monitoring observations are needed.
