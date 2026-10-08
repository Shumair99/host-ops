# Isolated Docker demonstration

Requires Docker Engine/Compose. Set a random `HOSTOPS_TOKEN` (at least 32 characters), then run:

```sh
docker compose -f demo/compose.yaml up --build
```

Open http://127.0.0.1:9090 and enter that token. The agent observes two fictional Nginx properties through Caddy. The host metrics describe the demo execution environment, not an independently instrumented production host. Internal `.demo.local` domains are Docker network aliases, not public DNS; TLS is deliberately not configured in this lab.

```sh
docker compose -f demo/compose.yaml stop site-b
# Observe public 502, stopped runtime and doctor evidence.
# After three failure cycles, an incident opens.
docker compose -f demo/compose.yaml start site-b
# After two successful cycles, it resolves.
```

The agent's Docker socket access is powerful even though the volume mount is `:ro`. Use this compose file only in an isolated development environment. The management and demo HTTP ports publish only on loopback. Nothing in this demo touches the real VPS.
