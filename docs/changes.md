# Infrastructure flight recorder

Each cycle snapshots selected runtime image/revision, ports, restart policy/count, start time, loaded proxy observation, DNS and overall health. Snapshots are deterministic: DNS addresses, route upstreams and JSON map keys are sorted. Latency noise is excluded.

`hostops diff [site] --since 24h` reads field-level before/after records. First observation establishes a baseline. Losing access is recorded as observation availability, not proof of a configuration removal. Restart counts and health are state changes; changes in proxy observation can include reachability as well as configuration. This release does not hash arbitrary filesystem files.
