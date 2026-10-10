# Retention Keeps Uncovered Writers (#7472)

Generation retention pruned any superseded generation past the age bound,
including uncovered writers: generations that wrote the graph and never
activated. The collector reconciliation probe needs those rows to mark the
scope dirty and force a full snapshot, so retention deleted the evidence
before the heal.

The candidate query now flags each candidate with the probe's own uncovered
definition (wrote, never activated, write newer than the scope's last
activated full, `-infinity` when the scope has no activated full) via a
`last_activated_full_write` CTE (`DISTINCT ON (scope_id)` over activated
fulls, newest ingested first — the same ordering as the probe's per-scope
`LIMIT 1`). Flagged rows leave the lock set and the prunable leg at any age,
including past the hard ceiling, and return on a lock-free `UNION ALL`
report leg capped at the same batch limit. The store counts them as
`Skipped["uncovered_writer"]`, which the existing cycle log and skipped
counter already carry; no new instrument. The targeted relock does not
re-check coverage: a full activating between select and relock could flip a
selected candidate (narrowable but not closeable without serializing
activation against retention, which the repo forbids). Tracked in #7903
with arbiter verdict arb-7472-defer-1; severity-table category: edge case.

The policy type, defaults, and normalize moved code-identically into
`generation_retention_policy.go` (doc comment extended) because the filter
pushed the old file over the 500-line cap; the dirgate ledger re-pins
335 to 336 with its regenerated mirror.

No-Regression Evidence (#7472): baseline is the base-revision candidate
query (commit 823778bbe1) planned on the seeded retention corpus (8 scopes,
5 superseded generations each, 400 content entities, 80 files, 100 filler
rows, old timestamps; params: soft cutoff now-7d, min-newer 0, batch limit
100, hard ceiling now-90d) on local PostgreSQL 18.6 (postgres:18 Docker):
53 plan lines, total cost 41.05..57.14. After is the new query on the
identical corpus and params: 74 plan lines, total cost 75.97..76.05. The
delta is one added MATERIALIZED `last_activated_full_write` scan+sort of
`scope_generations` plus the lock-free report leg; `fact_work_items` is
still read exactly once via `fact_work_items_status_idx`, every generation
re-check is still a primary-key probe, and no new per-row loop appears.
This is plan-shape evidence, not a wall-time claim: the candidate query
runs once per retention pass, not per key, so a single added bounded scan
keeps its budget. The regression test pins terminal counts on a 3-row
fixture: uncovered retained and skipped, covered pruned,
`GenerationsPruned=1`. No new index: the horizon scan filter matches no
narrower existing index and the ranked CTE already scans the same table.

Observability Evidence (#7472):
`eshu_dp_generation_retention_skipped_total{reason="uncovered_writer"}`
counts every skipped writer and the `generation retention cycle completed`
log carries it in `skipped_by_reason`; the regression test asserts the
`Skipped` map entry and the metrics reference documents the reason in the
same PR. Post-deploy, a nonzero `uncovered_writer` share means retention is
holding probe evidence; correlate with the collector's forced-full-snapshot
rate on the same scopes.
