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
`eshu.security_alert.org_fanout.*` counts for the claim.

`eshu_dp_security_alert_fetch_duration_seconds` records the org read as one
`success` sample taken when that read finishes, before the fan-out. Each
per-repository read records its own sample, so a claim that fans out adds one
sample per repository read.

A read that ends as `alerts_disabled` still records
`eshu_dp_security_alert_provider_requests_total{status_class=auth_denied}`
because the provider answered 403. Use `outcome=alerts_disabled` on the
fan-out counter to tell it from a credential failure; the fan-out continued.

A truncated org read whose allowlist holds no repository of the organization
is not a fan-out: it records `eshu_dp_security_alert_truncated_reads_total`
with `operation=list_org_alerts` and no fan-out counter or span attribute.

Repository names, alert URLs, and provider response bodies stay out of metric
labels.
