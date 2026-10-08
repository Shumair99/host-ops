# HostOps demonstrations

## Offline terminal walkthrough

The quickest demo needs no Docker, SSH, token or network:

```powershell
.\bin\hostops.exe dashboard --demo
```

Use `s` and Enter to inspect a fictional property, `t` to simulate an outage, `n` to see the opened incident, and `t` again to resolve it. `d` shows fictional disk accounting. `q` returns to the shell. See the full [terminal walkthrough](../docs/terminal-guide.md). The demo is explicitly labelled and uses in-memory history.

## Optional isolated Docker lab

Requires Docker Engine/Compose. This is an optional runtime integration exercise on a development machine, not the production VPS:

```sh
docker compose -f demo/compose.yaml up -d --build
docker compose -f demo/compose.yaml exec agent hostops dashboard --config /etc/hostops/sites.json --db /var/lib/hostops/history.db --no-collect
```

Compose runs a headless collector every ten seconds. The second command opens its terminal dashboard against the same SQLite database without starting another collector. There is no dashboard port or token. The lab includes two fictional Nginx websites through Caddy, with only the lab website HTTP port published on loopback at 8088. Internal `.demo.local` names are Docker aliases, not public DNS; TLS is deliberately absent.

In another shell:

```sh
docker compose -f demo/compose.yaml stop site-b
# After three failure cycles, the incident opens.
docker compose -f demo/compose.yaml start site-b
# After two recovery cycles, the incident resolves.
```

Exit the dashboard with q. Stop the lab with `docker compose -f demo/compose.yaml down`; the history volume is retained unless explicitly removed. Host metrics in this containerized lab describe its execution environment, not an independently instrumented production host.

Docker socket access remains powerful despite a `:ro` mount. Use this lab only in an isolated development environment.
