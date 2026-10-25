# Security Alert Org Fan-Out Telemetry

An `org` security-alert target reads at most `max_pages` pages of the
organization's open-alert stream. When that read truncates, the collector reads
each allowlisted repository on its own (#8805). These signals show how often
that happens and how each repository read ended. Other security-alert
collector metrics are in
[Ingestion And Collector Metrics](metrics-ingestion-collectors.md#security-alert-collector).

| Metric | Key labels | Use |
| --- | --- | --- |
| `eshu_dp_security_alert_truncated_reads_total` | `provider`, `operation` | Provider reads that hit `max_pages` before the alert stream ended. `operation=list_org_alerts` is a truncated org read (it triggers the per-repository fan-out); `operation=list_repo_alerts` is a repository read, including a fan-out read, that still truncated. |
| `eshu_dp_security_alert_org_fanout_repositories_total` | `provider`, `outcome` | Allowlisted repositories handled by the org fan-out after a truncated org read. `outcome` is `complete`, `truncated`, `failed`, `alerts_disabled` (a 403 saying Dependabot alerts are disabled for that repository; the fan-out continues), `rate_limited` (the fan-out stopped at a rate limit), `stopped` (an earlier read failed with a class other than `not_found` or `rate_limited`, so the fan-out stopped), or `skipped_cap` (past the 100-repository per-claim cap). |

The claim also records the outcome durably (#8943). Every `org` claim writes an
org read state with its generation: whether the org stream truncated, and each
allowlisted repository left unread with its reason. The `security_alert.observe`
span carries `eshu.security_alert.org_read_truncated` for the same flag, and
`eshu.security_alert.org_fanout.incomplete` is the unread count. To find the
targets whose latest read is cut short, query
`security_alert_target_read_states` joined on `ingestion_scopes.active_generation_id`
(the query is in
[Security alert parity](../security-intelligence-provider-alert-parity.md#org-read-state)).
The read path turns the stored reason into the `coverage.reasons` of an empty
answer for an unread repository, and
`eshu_dp_query_security_alert_empty_answers_total` counts those answers by
`coverage_state` and first reason.

Each affected fact names why in `collection_incomplete_reasons`. A non-zero
`outcome` other than `complete` means some allowlisted repositories of an org
target are still incomplete; the `security_alert.observe` span carries the
`eshu.security_alert.org_fanout.*` counts for the claim, plus
`eshu.security_alert.org_fanout.start_offset`, `.next_offset`, and `.lapped`: the
position, in allowlist order among the organization's allowlisted repositories,
where this claim started and where the next claim starts, and whether a lap is
in progress after this claim (#8946). The offsets are indexes, never repository
names. The walk stops at the first failing repository and labels the repositories
behind it `org_repository_fanout_stopped`. The start moves forward to the
repository after a failed read (`.next_offset` greater than `.start_offset`).
A failed read at the last position closes the lap: `.lapped` turns true and the
start returns to the top (0 and 0) and stays there until a claim reads every
repository or that last repository leaves the allowlist. Equal offsets are the
normal steady state: 0 and 0 with `.lapped` false is a claim that read every
repository or a target over the cap; a non-zero equal pair is a start settled
past a failing repository (`.stopped` true, `.rate_limited` false); 0 and 0 with
`.lapped` true is a target settled at the top. A start settles after at most one
stored generation per failing repository plus one, and the committer then
skips. With `.rate_limited` true the start stays where it was, so a standing
rate limit repeats the same window: raise the instance's `scan_interval` (the
parity reference gives the formula). The positions are stored as repository
names on the generation's read state row (`fanout_start_repository`,
`fanout_next_repository`, `fanout_lap_repository`; NULL means the top and no
lap), which is where an operator reads them: a lap in progress is
`fanout_next_repository IS NULL AND fanout_lap_repository IS NOT NULL` on the
newest generation's row. A target with more than 100 same-org allowlisted repositories is collected as
shards of at most 100 (#9019), so `skipped_cap` that is non-zero on every claim
of one scope now means a shard at the depth cap (more than 100 repositories
sharing a 32-bit hash prefix) that always reads the same first 100; the
collector logs one `security_alert.org_fanout_over_cap` warning at startup
with the repository count, the cap, and the `scope_id`.

Shard signals carry counts only. Each org claim's `security_alert.observe`
span has `eshu.security_alert.shard.count` (shards of its target, 1 when not
sharded), `eshu.security_alert.shard.repositories` (repositories of this
shard, 0 on a retirement), and `eshu.security_alert.retirement`. The
collector logs `security_alert.org_target_sharded` (INFO, once per sharded
target at startup: `shard_count`, `smallest_shard_repositories`,
`largest_shard_repositories`), `security_alert.scope_retired` (INFO, per
retirement claim: `scope_id`, `parent_scope_id`, `generation_id`), and
`security_alert.target_sharded_coordinator_skew` (WARN: a coordinator older
than the collector still plans a sharded target's base scope; upgrade the
coordinator). The coordinator logs `security alert planner retiring unplanned
org target scopes` (INFO, `retired_scope_count`) and, when one scan reaches
1,024 retirements, `security alert planner reached the per-scan retirement
bound` (WARN); its candidate read is Postgres store
`security_alert_shard_scopes`. To see which scopes a target has:
`SELECT scope_id, active_generation_id FROM ingestion_scopes WHERE scope_id =
'<scope_id>' OR parent_scope_id = '<scope_id>'`; a retired scope's active
generation has a read state with `repositories_total = 0`.

`eshu_dp_security_alert_fetch_duration_seconds` records the org read as one
`success` sample taken when that read finishes, before the fan-out. Each
per-repository read records its own sample, so a claim that fans out adds one
sample per repository read.

A read that ends as `alerts_disabled` records
`eshu_dp_security_alert_provider_requests_total{status_class=not_found}`:
since #8947 the 403 that says Dependabot alerts are disabled classifies as
`not_found`, not `auth_denied`, on every path. A repository-scope target that
gets it fails terminally with `not_found` (readiness shows
`terminal fetch failure: not_found (list_repo_alerts)`), and a rise in
`status_class=auth_denied` is a real credential or permission problem. On the
org fan-out use `outcome=alerts_disabled` to tell the disabled case from a
repository that is missing; the fan-out continues in both.

A truncated org read whose allowlist holds no repository of the organization
is not a fan-out: it records `eshu_dp_security_alert_truncated_reads_total`
with `operation=list_org_alerts` and no fan-out counter or span attribute.

Repository names, alert URLs, and provider response bodies stay out of metric
labels.
