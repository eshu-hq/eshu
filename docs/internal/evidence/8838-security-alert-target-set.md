# Security-alert target set per generation (#8838)

**A security-alert generation now records the repositories it collected, and
an empty answer for a repository outside the latest committed generation's set
reads `no_committed_generation` instead of `complete`.**

## Problem

The empty-answer read (#8803) took target membership from the current
collector configuration and committed state from "any committed generation".
A repository added to an allowlist, or a repository target whose `scope_id` was
reused for another repository, inherited the existing generation and read
`complete`, exact and fresh, before the collector had read it.

A second cause kept the window open for good in one case. The freshness hint
covered only the provider answer. When the added repository had no alerts, the
next claim produced the same hint, so `backfill.DecideUnchangedGeneration`
skipped the generation as unchanged. No generation would ever record the new
repository. `TestSecurityAlertTargetSetAllowlistChangeLive` proves the skip:
an equal-hint commit with a new set leaves one generation row.

## Change

- `scope.SecurityAlertTargetSet` is the sorted, deduplicated slug set, kept as
  canonical JSON so `ScopeGeneration` stays comparable.
- The collector sets it on each generation: `[repository]` for a repository
  target, or the allowlisted repositories of the organization for an org
  target. The set's hash enters the freshness digest, so every target commits
  one extra generation after the upgrade.
- The ingestion store writes `security_alert_target_set` and
  `security_alert_target_set_hash` to `scope_generations.payload`. There is no
  DDL.
- The collection-state read replaces the committed `EXISTS` with a top-1
  lateral over the latest committed generation and returns the requested slugs
  missing from its set. The classifier treats a target as uncommitted for the
  read when a slug the target covers is missing.
- A generation that recorded no set keeps the earlier answer.

## Performance Evidence

Performance Evidence: an `EXPLAIN (ANALYZE, BUFFERS)` shim on PostgreSQL 18.6
(the CI image) with all migrations applied. The data was 1,024 security-alert
targets with 2,000 committed generations each, plus 3 pending or failed
generations at the head of every fourth target, plus 20,000 git scopes with 50
generations each: 3,048,769 `scope_generations` rows, vacuumed and analyzed.
The new lateral walks `scope_generations_scope_latest_lookup_idx` newest first.
It removes at most 3 rows by filter and stops at the first committed row.

| Read | Before (EXISTS) | After (lateral) |
| --- | --- | --- |
| 1 target, warm | 18 buffers, 0.092 ms | 21 buffers, 0.125 ms |
| 1,024 targets, warm | 10,581 buffers, 14.963 ms | 12,362 buffers, 17.011 ms |
| 1,024 targets, cold | 10,581 buffers (1,868 read), 95.185 ms | 12,362 buffers (2,697 read), 142.722 ms |

The added cost is about 1.7 buffers per target: one heap fetch of the
committed row's payload. It does not grow with generation history. With a
5,000-repository target set, the payload is 150,077 bytes as text and 22,660
bytes stored (TOAST-compressed). The one-target read is 25 buffers and 0.34 ms
warm, and it returns the right membership.

The read runs only on an empty answer, inside the existing 1 s probe deadline.
Pages with rows skip it.

## Observability

Observability Evidence: no new signal. The existing
`eshu_dp_query_security_alert_empty_answers_total{coverage_state, reason}`
counter and the `security_alert_collection_state` stage log
(`collector_state=collecting`) report the new outcome as
`target_incomplete` / `no_committed_generation`. Neither carries a repository
name. The payload holds slugs the collector already stores in its facts.
