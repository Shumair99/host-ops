# Production readiness and improvement queue

Readiness has five explicit categories of 20 points: availability, deployment safety, recovery, security and observability. Every rule exposes its weight, observed state, evidence, investigation and priority. Passing rules earn their stated points. Missing, stale, failing or inapplicable observations earn zero; missing evidence makes the assessment provisional. Scores are assessment aids, not a certification or guarantee.

Fresh runtime/health means less than three minutes old. Independent watcher and resource observations must also be recent. Backup evidence must be within the configured RPO (24 hours by default); restore evidence must be health-verified within 30 days. The dashboard presents failed/unverified rules as a prioritized improvement queue within each property. A deployment notification alone does not prove a retained rollback image exists.
