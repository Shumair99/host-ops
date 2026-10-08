# Preflight and dependency impact

`hostops preflight reboot` checks Docker boot enablement, running Caddy/restart policy, current site health, application restart policy, measured capacity, backup freshness, reported restore evidence and any reported backup in progress. `hostops preflight <site>` scopes the application checks to that property. Missing evidence prevents a green recommendation; confirmed blockers say not to proceed. Passing observations are still not a guarantee of successful maintenance.

`hostops impact caddy`, `hostops impact docker`, `hostops impact host` and `hostops impact <site-or-container>` simulate the registered dependency graph. Static applications still depend on Docker when their Caddy proxy runs in Docker. Unregistered resources and hidden dependencies may expand the actual blast radius. No maintenance action is executed.
