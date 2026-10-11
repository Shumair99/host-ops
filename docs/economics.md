# Optional billing, allocation and capacity

Monitoring needs no billing configuration. Costs live under Tools (`a`) and remain reachable with `c`. Without a usable budget/provider observation, that view explains setup instead of displaying unknown allocations. Fee/margin columns stay hidden until you configure client fees.

## Set up from the dashboard

Open **Tools → Costs / setup**, focus the content with Enter or Right, then press **Enter** on **Set up billing** (or **S**). Choose **Monthly budget**, enter the amount, then select **GBP**, **EUR** or **USD** using Left/Right or clicking an option. Select **Save settings**. Tab/Shift+Tab moves between fields; Ctrl+S also saves. An existing currency configured through the CLI/settings is retained as an additional choice. Estimates update immediately. The same settings remain available for editing after setup. Own hardware supports an optional budget or disabled cost tracking.

For Hetzner or Hostinger, choose the provider and use an existing token environment variable or enter a masked token in the form. Leave Resource ID blank to list the account's servers/subscriptions and choose the correct resource. Entered tokens live only in the running dashboard's context; they are not written to settings, history or the child-process environment. On the next launch, provide the token again or use the named environment variable. API operations remain GET-only. A failed pricing read remains visible and can be retried from settings.

New-connection onboarding offers to open this same billing form. Setup can be skipped and completed from the cost page later. Saving changes only local settings; it does not purchase or modify provider services. Command-line configuration remains supported below.

## Provider API setup

Select one billing source and resource, read its prices/subscription or explicit budget, retain evidence locally, then estimate property allocation. Hardware size and CPU activity cannot establish what you pay. API readers issue GET requests only. Tokens stay in environment variables or the dashboard session and are never saved in the manifest/database. API prices are cached for six hours; `billing sync` forces a read. Costs refresh billing when opened if the cache has expired; billing is not polled at telemetry frequency.

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

Site estimates value the **server capacity actually consumed**, rather than dividing the whole bill between sites. The default weighting is 50% CPU and 50% RAM. A workload averaging 10% of all server CPU capacity and 20% of host RAM represents 15% of the monthly budget: £15 on a £100 budget. Idle workloads can have a measured zero estimate. Adding an unmeasured workload does not increase or reduce the estimates of measured workloads.

CPU uses workload CPU divided by all host cores (container CPU can exceed 100% because 100% represents one core). RAM uses workload bytes divided by host RAM, not the container's memory limit. `host.cost_weights` remains non-negative and totals 1. A legacy `equal` component is split equally into CPU/RAM weighting; it no longer distributes a fixed part of the bill. Effective weights are displayed. CPU/RAM weighting is a capacity valuation, not a provider tariff; disk, traffic and backup usage are not measured by this model, even when backup charges are included in the budget.

The dashboard shows measured site use, shared/unmeasured usage and unused capacity separately. **The full hosting bill remains payable**. The unassigned budget, when shown, includes both spare capacity and usage that cannot be attributed to a measured site; these amounts are not savings. If host and site readings disagree, the shared breakdown stays unknown instead of forcing a balance. Conflicting totals above physical capacity leave site estimates unknown.

Estimates use up to the latest 1,000 host and workload records within 24 hours, matching each workload reading to host capacity observed within 90 seconds. Only readings overlapping the retained host window enter the estimate. The actual window and coverage are displayed; this is a projection of recent observed usage to a month, not a measured monthly invoice. A current observation must be within three minutes; absent, stale or invalid required counters remain unknown. Missing resource weights are not redistributed. Current per-workload CPU measurements are available for Docker/Podman; unmeasured runtimes remain unassigned. Confirmed shared containers/services and configured redirects are counted once, with their linked addresses retained. A separate application on a www or analytics address keeps its own estimate.

Site `business.monthly_fee` supplies optional revenue. Its configured `host.currency` must match provider pricing for margin comparisons. No exchange rate is assumed; incompatible fees are omitted. Client contracts and actual invoice expenses are not inferred.

## Capacity

p95 requires at least 20 observations from the latest 1,000 host samples. Disk growth uses the oldest/latest retained observation within 30 days and requires at least 24 hours and positive growth; older evidence is no longer lost when recent samples fill the query limit. Usage already above 85% is reported directly. This projection is a scenario, and no additional-site count is fabricated.
