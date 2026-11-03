# 9871 claim-plan evidence

Why the claim query plans on the family index after a bulk admission, what
the post-commit ANALYZE changes, and the measured before/after on the same
corpus.

## Setup

Local PostgreSQL 18 over socket on the dev box. Corpus: 100,011 work items
across 200 scopes bulk-loaded into `workflow_work_items` with no ANALYZE, the
shape from the #9727 contention harness. Statement under test is the
production claim query constant (plan guard derives EXPLAIN from the shipped
text with a prefix check, not a copy).

## Measurements

Cold (never analyzed): planner picks `family_queue_depth_idx` with `rows=1`
estimates, scanning 49,750 rows across 200 loops; EXPLAIN (ANALYZE, BUFFERS)
11,522 ms. Warm (after ANALYZE): planner picks `fair_claim_idx`; 3.939 ms;
warm claims p50 4.8 ms, max 6.3 ms.

After the fix (post-commit ANALYZE gated on never-analyzed, production
`EnqueueWorkItems`, no manual ANALYZE): first claim plans on `fair_claim_idx`
with no sequential scan; post-admission claims n=20 p50 4.7 ms, max 6.9 ms.
Warm-plan p50 is not regressed (4.7 ms vs 4.8 ms). Six concurrent claimers
during ANALYZE overlap with zero errors (wall 57 to 81 ms across runs).

Performance Evidence (#9871): cold 11,522 ms family-index plan vs warm
3.939 ms fair-claim-index plan on PG 18, 100k items / 200 keys, freshly
bulk-loaded never-analyzed table; post-fix first claim via production
admission p50 4.7 ms max 6.9 ms (n=20), warm p50 unchanged. The claim path
(`ClaimNextEligible`) is untouched, so per-claim cost is zero; the one
bounded ANALYZE runs once per cold admission, post-commit, SKIP_LOCKED,
non-fatal and counted. Rejected by measurement: per-table autovacuum
settings (floor is the global autovacuum_naptime, ~37 s) and a query-shape
change (dropping the family index is hot-path schema surgery for a
statistics problem).

Observability Evidence (#9871): new counter
`eshu_dp_workflow_work_items_stats_analyze_total` (site x outcome), span
`workflow.work_items.stats_analyze`, and `workflow.stats.*` log keys; the
`verify-telemetry-coverage.sh` gate agrees and the telemetry package tests
pass. Coverage rows added to the collectors and signals docs.
