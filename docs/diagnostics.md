# Evidence and diagnosis

`hostops doctor [site] --config local/hostops.json` derives runtime/proxy checks from automatic inventory and runs separate public DNS/TLS/HTTP checks. Proxy evidence identifies active versus declared configuration. Independent backend HTTP is labelled not configured in automatic mode. The explicit registered workflow can perform operator-configured local HTTP checks. GET probes discard response bodies. Container inspections select metadata without environment values or health log output.

Every finding has evidence, confidence, an affected layer and an investigation. Observation failures remain unknown. A stopped container is confirmed only from successfully decoded state. Public HTTP uses the executing machine's network; running the CLI on the production VPS is not independent outage detection. TLS verifies the trust chain and hostname; it never disables verification.

## Content-aware findings

The dashboard surfaces a concise finding on Overview, Sites and the property passport. Issues begins with per-domain findings; the inspector and incident capsule retain the evidence and next checks. `Observed` means the specific fact was measured, `Likely` marks a bounded inference, and `Unconfirmed` means no cause is established. An observed failure is not automatically a proven underlying cause. Base and `www` domains keep separate diagnoses; one address cannot borrow another's local HTTP result.

| Evidence | Finding / boundary |
| --- | --- |
| Successfully inspected stopped runtime | Runtime stopped; why it stopped is unconfirmed |
| Active Caddy JSON lacks expected domain/upstream | Missing route or mismatched upstream; admin access failure stays unknown |
| Typed resolver failure | No usable address, DNS timeout, or resolver failure; no expiration guess |
| Expected-IP comparison fails | DNS differs from configured expectations; intentional CDN/migration is possible |
| Typed certificate validation failure | Expired, not yet valid, wrong hostname, or untrusted certificate |
| TCP connection refused | Observed refusal; firewall/listener/component cause unconfirmed |
| Local HTTP passes; public 502/504 | Likely gateway/upstream path problem; not proof of incorrect Caddy configuration |
| HTTP 401/403/404 | Actual endpoint status and relevant auth/path checks; not a stopped-process claim |
| Redirect loop, credentials or HTTPS downgrade | Specific rejected redirect policy |
| Generic timeout/unclassified transport error | Cause remains unconfirmed |

DNS lookup failure blocks redundant TCP/TLS probes in that network cycle; the public HTTP request retains its own result. A failed TCP connection blocks the TLS probe. Skipped downstream checks identify the blocking check. Network probe timestamps remain visible when cached; a new HTTP failure can identify DNS/TLS trouble even when the earlier network observation passed. Missing/stale collector evidence cannot retain a confirmed runtime diagnosis.

## Public registration evidence

Failing public endpoints may trigger a read-only RDAP lookup of the **registrable parent domain**. The bundled Public Suffix List handles names such as `app.example.co.uk`; IP addresses, local names, unknown/private suffixes and non-ASCII input are excluded. ASCII IDN/punycode domains remain eligible when their public suffix is recognized. No private subdomain name is sent to a registry.

The client fetches [IANA's HTTPS bootstrap](https://www.iana.org/assignments/rdap-dns/) and queries its listed HTTPS registry service. It accepts only JSON domain objects matching the requested domain. It retains selected lifecycle statuses, the top-level expiration event, source URL and observation time. Contacts, entities, response text, links and registrar-account data are not retained. A matching RDAP `errorCode: 404` is reported as no registry record; generic 404 pages, malformed/oversized responses, rate limiting and unsupported services remain unavailable.

`client hold` / `server hold`, redemption, pending deletion, elapsed registry expiration and auto-renewal grace periods are reported as **registry facts**. An elapsed date does not prove loss of ownership or that expiration caused DNS failure. A hold confirms the reported hold, not its reason. Registry and registrar renewal dates can differ during [expiration grace periods](https://www.icann.org/resources/pages/expired-not-working-2013-08-30-en). DNS failure by itself never means an expired registration. HostOps cannot verify that an operator owns a domain, paid a renewal invoice, or intends to keep it.

Lookups are limited to three seconds, two concurrent requests, 1 MiB per domain response / 4 MiB bootstrap, and 1,024 in-memory domain entries. Matching successful records cache for one hour, no-record results for 15 minutes, and unavailable results for five minutes; bootstrap data caches for 24 hours. The observation time stays attached to cached evidence. Redirects, credentials, cookies, environment proxies, insecure TLS, nonstandard ports and private/loopback/link-local/special-address connections are excluded. DNS is resolved once and the validated public IP is used for dialing. Unavailable registration evidence is supplemental (`SKIP`), so registry downtime cannot itself make a website unhealthy. Set `HOSTOPS_NO_RDAP=1` to disable all registration lookups.

The connected VPS and configuration are trusted operator inputs. Public endpoints come from discovered domains or explicit health preferences. Health checks prove responses, not complete business journeys. See [discovery](discovery.md) for source coverage, automatic status semantics and unsupported routing.
