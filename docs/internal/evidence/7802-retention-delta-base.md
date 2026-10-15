# Retention Keeps Delta-Active Bases (#7802)

Generation retention pruned superseded generations by age and rank without
reading `scope_generations.is_delta`. For a scope whose active generation is
a delta, the newest full generation is the only record of unchanged files;
past the 90-day hard ceiling it became prunable, and with it any rebuild
from facts lost its base.

The candidate query now flags each candidate with a `delta_base` definition:
the newest full generation per delta-active scope (`DISTINCT ON (scope_id)`
over non-delta generations, newest ingested first — the repository's
newest-generation ordering) plus every generation ingested after it (the
delta chain). Flagged rows leave the lock set and the prunable leg at any
age, including past the hard ceiling, and return on a lock-free `UNION ALL`
report leg capped at the same batch limit, oldest-first. The store counts
them as `Skipped["delta_base"]`, which the existing cycle log and skipped
counter already carry; no new instrument. The targeted re-lock re-checks the
same definition over its scope-bounded horizon, so a delta that activates
between selection and re-lock still shields its base at lock time. Residual,
same class as #7903: a delta that commits after the re-lock's snapshot still
flips a locked member before the prune commits; closing that would serialize
activation against retention, which the repo forbids.

The targeted candidate query moved code-identically into
`generation_retention_relock.go` beside its only consumer because the guard
pushed `generation_retention_sql.go` toward the 500-line cap; no new file,
so no dirgate re-pin.

No-Regression Evidence (#7802): baseline is the pre-fix candidate query,
which prunes a 100-day base full and a 95-day chain delta under a 90-day
ceiling (original two-scope `TestGenerationRetentionDeltaBaseLive` RED:
`GenerationsPruned=3`, base and chain gone, `Skipped` empty). After is the
new query on the three-scope fixture (one delta-active scope with base plus
chain, one overlap scope whose chain delta is both uncovered and
delta-protected, one active-full scope with one old generation; params: soft
cutoff now-7d, min-newer 24, batch limit 10, hard ceiling now-90d) on local
PostgreSQL 18.6 (postgres:18-alpine throwaway): `GenerationsPruned=1`
(only the active-full scope's old generation), bases and chains kept,
`Skipped["delta_base"]=3` with `Skipped["uncovered_writer"]=1` reported
exactly once. Reverting the delta-leg uncovered exclusion makes the same
test FAIL with `Skipped["delta_base"]=4`, proving the overlap arm.
Plan shape on a five-scope delta-active corpus with identical params:
`EXPLAIN (ANALYZE, BUFFERS)` shows `delta_base` folded into `eligible` (one
merge join, no added CTE scan), `fact_work_items` still read exactly once
via `fact_work_items_status_idx`, and the new report leg is a CTE scan with
a `delta_protected` filter plus sort and limit: 0.79 ms, 26 shared buffers
at the top node. The candidate query runs once per retention pass, not per
key, so the added bounded join keeps its budget. No new index: the base
lookup is one `DISTINCT ON` over `scope_generations` per pass, served at
scale by the primary key and `scope_generations_scope_latest_lookup_idx`
(the 16-row fixture plans sequential scans, as expected at that size).

Observability Evidence (#7802):
`eshu_dp_generation_retention_skipped_total{reason="delta_base"}` counts
every kept base and covered chain row (a both-flags chain row counts once
as `uncovered_writer` instead) and the cycle log carries both reasons in
`skipped_by_reason`; the regression test asserts the `Skipped` map entries
and the metrics reference documents the reason in the same PR. Post-deploy, a nonzero `delta_base` share means retention is
holding delta-active bases; correlate with the count of delta-active scopes.
Like the uncovered-writer leg, the report cap makes the count a floor, not a
census, when protected rows exceed the batch limit.
