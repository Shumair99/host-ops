# Evidence and diagnosis

`hostops doctor [site] --config local/hostops.json` derives runtime/proxy checks from automatic inventory and runs separate public DNS/TLS/HTTP checks. Proxy evidence identifies active versus declared configuration. Independent backend HTTP is labelled not configured in automatic mode. The explicit registered workflow can perform operator-configured local HTTP checks. GET probes discard response bodies. Container inspections select metadata without environment values or health log output.

Every finding has evidence, confidence, an affected layer and an investigation. Observation failures remain unknown. A stopped container is confirmed only from successfully decoded state. Public HTTP uses the executing machine's network; running the CLI on the production VPS is not independent outage detection. TLS verifies the trust chain and hostname; it never disables verification.

The connected VPS and configuration are trusted operator inputs. Public endpoints come from discovered domains or explicit health preferences. Health checks prove responses, not complete business journeys. See [discovery](discovery.md) for source coverage, automatic status semantics and unsupported routing.
