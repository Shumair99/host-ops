# Security policy

HostOps is currently pre-release. Security fixes target the current source branch; older snapshots and locally built binaries should be rebuilt after fixes. This is not a guarantee that all vulnerabilities have been found.

After public GitHub publication, report vulnerabilities privately through the repository's **Security → Advisories → Report a vulnerability** facility. If private reporting is unavailable, ask the maintainer for a private channel without posting exploit details, credentials or real infrastructure evidence in a public issue. No public contact address is configured yet.

Include the affected commit/platform, a minimal synthetic reproduction, required permissions and expected impact. Keep real SSH identities, tokens, private host inventories and raw logs out of reports. Response times are best effort during pre-release development.

The [security boundaries](docs/security.md) describe SSH authentication, local ACLs, collector protocol limits, privileged installation and trust in the connected server. Open source and local execution make the implementation reviewable; they do not remove transport, dependency, local-user or privileged-installation risks. Checksums detect corruption, not publisher identity.

CI runs tests, vet and the official [Go vulnerability checker](https://go.dev/doc/security/vuln/). This detects known issues in reachable dependency code; it does not replace design review or live platform testing.
