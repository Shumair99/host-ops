# Understanding the operations tools

## Impact: what shares the resource you plan to change?

Impact includes inventoried containers, network attachments, storage mounts, recorded Compose startup dependencies, established local service connections and unmapped outbound peers. Each relationship says how it was observed. A peer is not labelled as a database merely because of its port number. Host, proxy, workload, network and storage resources can be selected to show affected properties.

Proxy networks and container names are discovered from the inspected host; no particular name or shared-network layout is required. Shared membership does not imply an app dependency. Database relationships are followed from recorded startup dependencies and observed connections. Ambiguous backend aliases are resolved from the proxy's DNS namespace and must match one inspected container IP, otherwise they remain unresolved.

Separate applications and database containers remain visible. Coverage gaps are explicit: idle connections, remote service internals, inaccessible namespaces and dynamically generated dependencies cannot be proved complete from an inventory snapshot. Impact does not deliberately fail any resource.

## Readiness: how much operational evidence is recorded?

The full weighted checklist adds up to 95, not 100. Availability contributes 20 points, deployment safety 15, recovery 20, security 20 and observability 20. This is evidence coverage, not a website health percentage. A website can respond correctly while its backups, restore tests, source revision or independent monitoring are unrecorded.

The dashboard displays an evidence percentage plus earned/applicable points. Skipped checks are removed from the maximum; they earn no points. Unknown evidence makes the assessment provisional. Failed checks and unknown checks appear separately in the point breakdown. Caddy-only redirects skip requirements for a separate application container. [Readiness rules and freshness windows](readiness.md) explain what raises the score.

## Recovery: recorded backup jobs and isolated restore tests

Recovery automatically scans recorded deployment directories and persistent mounts for backup-area files with SQLite, gzip, ZIP or tar signatures. It shows paths, sizes and modification times. A detected artifact produces a warning with `artifact_detected` confidence, never a restore or data-freshness pass. Scans are bounded, ignore symlinks and do not extract archives. Provider snapshots and inaccessible/offsite repositories are not discovered by this file scan.

Backup jobs can write a standard report under the SSH user's `~/.local/state/hostops/recovery/`. HostOps discovers it automatically each inventory scan; no manual import is needed per run. This is the authoritative reporting contract for completed jobs and restore tests. Existing imports remain supported. Unknown means required evidence is absent or unusable, not proof that no backups exist.

* **Backup:** the latest reported job succeeded, failed or is in progress.
* **Restore:** a reported isolated restore included health verification and an evidence reference. Tests older than 30 days are marked for review.
* **RPO (recovery point objective):** acceptable data loss expressed as a time window. The default target is 24 hours. HostOps compares backup completion age with this target; it does not independently measure how current the data inside the backup is.
* **RTO (recovery time objective):** acceptable time to recover service. The default target is 15 minutes. HostOps compares the reported restore's start/completion duration with the target. Detection, provisioning and other real incident steps are covered only if included in those timestamps.
* **Confidence:** `unverified` without a verified restore report; `reported_verified` after a qualifying operator/pipeline report. This is not independent artifact verification.

To supply real evidence:

1. Run the existing backup job and verify that its expected files/data were produced. Record actual outcome, completion time, size and destination. Local copies survive a bad app deployment but share the VPS failure domain; offsite copies provide a separate location.
2. Restore that backup into an isolated test environment with a separate database/volume and no production routing. Check required pages, records and application health. Record the test's actual start/end time, recovered data point, backup ID and short evidence reference.
3. Find the registered site ID with `hostops sites --config local/hostops.json --no-collect --json`. Import the reports into the same local database used by the dashboard. When using `--db`, pass the same path to both imports and the dashboard.
4. Refresh Recovery. A successful recent backup can pass Backup and RPO while Restore and RTO remain unknown. A verified restore supplies separate Restore/RTO evidence; a duration exceeding the target fails RTO even when restoration itself succeeded.

JSON formats and import commands are in [recovery.md](recovery.md). Do not submit sample claims as real evidence. Repeated IDs are idempotent; use a new job/test ID for each new execution. For a workflow running on the VPS or in CI, export its small JSON status report, transfer it to the machine holding HostOps's database, then import it there. HostOps does not offer an unauthenticated evidence receiver on the VPS.

## Preflight: a check before maintenance

The terminal's default target is `reboot`. It checks Docker boot enablement, Caddy's running/restart state, affected site health and app restart policies, backup freshness, restore evidence, in-progress backup reports, and measured capacity/disk headroom. It shows the expected host impact and a recommendation. It does not reboot, deploy or restore anything.

An enabled Docker service and `unless-stopped` policies are useful startup evidence, but not proof of recovery from every failure. A deliberately stopped container remains stopped with `unless-stopped`. A pre-existing down/retired site can fail the preflight even though the reboot would not cause that site's problem; review that context rather than treating every failure as a new incident. Unknown recovery evidence means the tool cannot establish that part of maintenance safety.

## Security: hardening observations, not a vulnerability count

The distribution counts posture checks and port bindings. A warning is an opportunity to review a setting, not proof of compromise or an exploitable vulnerability. Unknown means evidence or container ownership is missing. Several domains mapped to one app container are checked once and retain their endpoint scope. A fresh status string alone does not prove the hardening settings were collected.

The [deployment assessment principles](deployment-principles.md) apply equally to every host. Host-specific connection settings do not change those criteria.

| Finding | Meaning | Typical improvement |
| --- | --- | --- |
| Observed process permissions | Effective UIDs are mapped into the container namespace. All observed non-root processes pass. A root Nginx supervisor with non-root request workers still warns: the lower-permission workers are explained, while the remaining root process stays visible. Root app/worker processes also warn; incomplete observations stay unknown. | Review the actual root process. Use a supported non-root image or runtime user after testing ownership and ports. A snapshot does not prove that an app cannot elevate privileges later. |
| Privileged execution | `privileged: true` removes much of the normal container isolation. | Remove it when the app has no demonstrated need; test the narrow permissions it actually requires. |
| Memory limit / availability | No container memory cap means a runaway app can exhaust shared VPS memory. | Set Compose `mem_limit` based on measured peaks plus headroom; a limit set too low can kill the app. This is mainly availability isolation. |
| Writable root filesystem | The container can modify its image filesystem. | Test `read_only: true`; provide volumes/tmpfs only where the application needs writes. Keep database/data volumes writable. |
| Image version reference | Untagged images and latest-style tags can resolve to different builds later. | Use a commit tag or digest and maintain an update process. This helps reproducibility and rollback; pinning alone does not establish that the image is patched. |
| Docker socket mounted | Docker control access can grant substantial control over the host. | Remove the socket mount where not required; a read-only socket bind is not a read-only Docker API. |
| Published ports | Loopback-only bindings and expected Caddy 80/443 bindings are distinguished from broader publications. | Prefer app-to-proxy Docker networking; bind host-only diagnostic ports to loopback. Review firewall rules and required public access separately. |

Process inspection retains identities and recognized roles, not arguments or environment values. This view does not scan CVEs, test external reachability, audit SSH or validate the firewall. Passing checks establishes only the facts inspected. See Docker's [runtime documentation](https://docs.docker.com/engine/containers/run/), [resource limits](https://docs.docker.com/engine/containers/resource_constraints/), [Compose service settings](https://docs.docker.com/reference/compose-file/services/) and [image best practices](https://docs.docker.com/build/building/best-practices/).

## Site names and domain ordering

The dashboard prefers domains throughout, including manually named sites. Stable IDs retain existing history. Confirmed apex/www redirects share one Readiness assessment; Health log retains their independent address checks. A broken redirect remains visible in the shared assessment. Separate subdomain applications keep their own assessment.

Domain tables, inventory routes and observation maps place a base domain immediately before its www variant, independently of their generated IDs or custom names. Chronological event feeds retain time order; resource rankings retain measured-value order. Related-domain inspectors place the property's www entry immediately after the parent and keep nested www pairs together.
