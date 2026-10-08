# Evidence and diagnosis

`hostops doctor [site] --config local/hostops.json` collects independent Docker, runtime, application health, host-local HTTP, loaded Caddy route, DNS, TLS and public HTTP observations. GET probes discard response bodies. Container inspections select metadata without environment values or health log output.

Every finding has evidence, confidence, an affected layer and an investigation. Observation failures remain unknown. A stopped container is confirmed only from successfully decoded state. Public HTTP uses the executing machine's network; running the CLI on the production VPS is not independent outage detection. TLS verifies the trust chain and hostname; it never disables verification.

Site configuration is an operator-provided allowlist of endpoints. This CLI is for a trusted operator, not an endpoint accepting arbitrary untrusted URLs. Health checks prove configured endpoint responses, not complete business journeys.
