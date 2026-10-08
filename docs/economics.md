# Cost and capacity estimates

Set host `monthly_cost`, `monthly_backup_cost`, ISO `currency` and each site's `business.monthly_fee` in the private manifest. With no positive configured host cost, estimates and margins remain unknown. These calculations allocate infrastructure cost across registered sites, not accounting expenses or profitability.

Default cost weights: equal share 0.4, average memory share 0.3, average CPU share 0.3. Configure `host.cost_weights` to non-negative weights totaling 1. Shares normalize across registered sites; a missing/zero total uses equal share for that dimension. Unmanaged databases and shared services are treated as shared overhead. The terminal and `hostops costs --json` expose the formula, weights and number of samples used.

Capacity uses measured host metrics. p95 requires at least 20 observations. Disk growth projection requires 24 hours of measurements and positive observed growth; current usage already over 85% is reported directly. Query windows are bounded to 1,000 samples. No fabricated number of additional websites is returned. Noisy-neighbour flags compare memory share to revenue share and require measured aggregate memory plus configured revenue.
