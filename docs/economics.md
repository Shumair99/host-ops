# Optional billing, allocation and capacity

Monitoring needs no billing configuration. Costs live under Tools (`a`) and remain reachable with `c`. Without a usable budget/provider observation, that view explains setup instead of displaying unknown allocations. Fee/margin columns stay hidden until you configure client fees.

## Provider API setup

Select one billing source and resource, read its prices/subscription or explicit budget, retain evidence locally, then estimate property allocation. Hardware size and CPU activity cannot establish what you pay. API readers issue GET requests only. Tokens stay in environment variables and are never saved in the manifest/database. API prices are cached for six hours; `billing sync` forces a read. Costs refresh billing when opened if the cache has expired; billing is not polled at telemetry frequency.

Set `HOSTOPS_HETZNER_TOKEN` or `HOSTOPS_HOSTINGER_TOKEN` in your terminal environment. Use a read-only Hetzner project token. Hostinger tokens inherit account permissions; HostOps uses its subscription-list GET endpoint. Do not pass token values as CLI arguments or put them in settings. `--token-env NAME` supports an alternative variable.

List existing resources, select the server/subscription belonging to the monitored host, and save its ID:

```powershell
.\bin\hostops.exe billing list --provider hetzner
.\bin\hostops.exe billing set --provider hetzner --resource SERVER_ID
.\bin\hostops.exe billing sync
.\bin\hostops.exe costs
```

For Hostinger, use `--provider hostinger` and its subscription ID from `billing list`. All commands accept `--config FILE`; show/sync accept `--db FILE`, and list/show/sync support `--json`. Restart an open dashboard after changing settings. `billing set` changes only local metadata and preserves connection/site settings. It does not purchase a plan, resize a server or change a subscription.

### What the amounts mean

Hetzner's [billing FAQ](https://docs.hetzner.com/cloud/billing/faq/) bills existing Cloud servers by allocated hours, rounded up, with a monthly cap. Powered-off or lightly used servers are still billed. The [Cloud API](https://docs.hetzner.cloud/reference/cloud) supplies current server type, location and account pricing. HostOps shows the gross monthly run rate for the server and enabled server backups. Its server-only month-to-date estimate assumes the current plan throughout the UTC month; it is not a reconciled invoice. IP addresses, volumes, snapshots, excess traffic, credits and invoice adjustments are excluded.

Hostinger's [API reference](https://developers.hostinger.com/?PID=2003851) provides a subscription's billing period, currency, current amount and renewal amount in minor units. HostOps normalizes its current period to a monthly equivalent and shows renewal separately. Day/week periods use an average calendar month. Add-on subscriptions and final invoice details are excluded. Discounts or tax adjustments cannot be guessed from a plan name.

Readers were tested against API contract fixtures. Live billing remains unverified until an account token and resource are supplied. Other providers currently use explicit manual billing details.

## Manual and own-hardware settings

For another provider, enter your actual monthly budget or an equivalent from its bill:

```powershell
.\bin\hostops.exe billing set --provider manual --monthly 12.50 --currency GBP
```

A home server works with costs disabled:

```powershell
.\bin\hostops.exe billing set --provider self-hosted
# Optional budget for electricity, equipment and other expenses:
.\bin\hostops.exe billing set --provider self-hosted --monthly 8 --currency GBP
```

Explicit `--monthly 0 --currency GBP` is a known zero budget. A missing budget remains unspecified. Existing `host.monthly_cost`, `monthly_backup_cost` and `currency` still form a budget when no `host.billing` source is selected. Manual sources add `monthly_backup_cost` to their base budget; API readers use their documented components rather than adding unrelated configured charges.

## Estimated allocation

Weights default to equal share 0.4, average memory share 0.3 and average CPU share 0.3. `host.cost_weights` must be non-negative and total 1. Incomplete/zero aggregate usage uses equal shares for that resource, stated as an estimate. The window is at most the latest 1,000 per-property records in 24 hours; unmanaged services are shared overhead. Base/www pairs allocate once. Other domains sharing a backend are not independent allocations; this is a property budget estimate, not accounting.

Site `business.monthly_fee` supplies optional revenue. Its configured `host.currency` must match provider pricing for margin comparisons. No exchange rate is assumed; incompatible fees are omitted. Client contracts and actual invoice expenses are not inferred.

## Capacity

p95 requires at least 20 observations from the latest 1,000 host samples. Disk growth uses the oldest/latest retained observation within 30 days and requires at least 24 hours and positive growth; older evidence is no longer lost when recent samples fill the query limit. Usage already above 85% is reported directly. This projection is a scenario, and no additional-site count is fabricated.
