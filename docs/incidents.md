# Incident evidence

Three consecutive critical observations open an incident. Two passing or warning observations resolve it; unknown observations break both streaks without claiming recovery. Agent and watcher observations are distinct sources. State survives restarts. Duplicate watcher submissions do not advance the failure threshold.

When at least two registered sites all report a failed Caddy runtime check, the agent opens one shared Caddy incident containing their site IDs. A separate watcher groups failed TCP connections only when all registered properties are observed and every affected domain resolves as expected; this is a possible shared public-path failure with medium confidence, since the observer's own network can also fail. Partial watcher batches and mixed dependency observations never establish shared recovery. Separate site incidents are evaluated when the shared dependency is passing.

If a site goes from healthy to critical, the doctor checks for an image change and completed deployment notifications within five minutes. Findings describe association and include investigation steps; they do not claim the deployment caused the failure. The first regression evidence is retained when the incident threshold is reached.

The incident list shows the latest state of each incident, including open incidents older than 24 hours. Site filtering includes shared incidents affecting that site. Up to 1,000 recent incident entities are returned.

Export a JSON evidence capsule through the dashboard or CLI:

```sh
hostops capsule INC-example --config local/hostops.json --db local/hostops.db > local/incident.json
```

On the terminal Incidents view, press `w` to export the newest listed incident to `local/capsules/<incident-id>.json`. Its open/resolved state is shown first; symptoms describe the observations when the outage opened. Capsules include symptoms, diagnoses and up to 100 recent records per kind from events, changes and deployment notifications near the incident. Raw logs, environment values, runtime dumps and manifests are excluded. Common credential patterns, IPv4 addresses and private paths are redacted. Names, IDs and public domains remain; review before sharing. Export creates a local artifact and never publishes it. There is no management HTTP endpoint.
