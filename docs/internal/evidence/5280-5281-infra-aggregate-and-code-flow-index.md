# Infra Aggregate Anchoring, Code-Flow Repo Index, and #5278 Full-Corpus Proof

Focused local proof for the combined follow-up PR to console-recovery epic
#5267: #5281 (graph infra aggregates), #5280 (Postgres code-flow read index),
and the full-corpus performance proof deferred from the merged #5278 fix.

Backend: NornicDB v1.1.11 base (`eshu-nornicdb-pr261:149245885258`,
commit `1492458852588c884c32f70d27ea2ee07086769c`) + PostgreSQL 18, isolated
Compose project `eshu-5279-81`, own ports/volumes. Corpus: 41 representative
repositories, 91,335 graph nodes / 104,744 edges, 330,474 Postgres
`fact_records` (5,653 of the graph nodes are infra-labeled).

## #5281 — infra resource aggregates: per-label anchoring

Performance Evidence: `GraphInfraResourceAggregateStore` (count / bucket /
inventory) previously ran a single unlabeled `MATCH (n) WHERE (n:A OR n:B ...)`
scan — the same whole-graph-scan defect fixed for infra/resources/search in
#5278. On the 91,335-node corpus, measured directly against the pinned NornicDB
backend over Bolt HTTP:

| Read | Old (`MATCH (n) WHERE OR-of-labels`) | New (per-label `CALL { UNION ALL }`) |
|---|---:|---:|
| count(n) total | 380.9 ms cold | 90 ms cold (via per-label + Go sum) |
| grouped bucket (by provider) | 410.9 ms cold | 1.2 ms cold |

Exact-equivalence (old vs new, same corpus) held for every filter dimension —
independently re-verified by an adversarial reviewer running the old
whole-graph shape against the new per-label shape live: total = 5,653; by-label
16 buckets; by-environment 1; by-resource-service 108; by-resource-category 11;
and the scoped path (a three-repository grant) total = 5,375 with identical
provider/label buckets. By-provider equivalence held for both provider
expressions the code emits: the category-filtered simple expression
(`n.provider`) yields the 10-bucket distribution `aws=1249, unknown=4375, …`,
and the default all-categories nested-CASE expression yields its own 2-bucket
distribution on this corpus — old == new in both. (The all-categories
expression renders one bucket as SQL-null rather than `unknown` on this
NornicDB build, but that expression is unchanged by this PR and Go maps null
and `unknown`/`""` identically for old and new, so equivalence is unaffected;
tracked separately as pre-existing bug #5283.) The old whole-graph scan
cost grows with total graph size; the new per-label scans are bounded by the
infra-label population, so the gap widens on larger corpora.

The `CALL { ... }` wrapper and per-branch grouping are load-bearing, not
stylistic — both work around NornicDB v1.1.11 bugs measured directly and now
documented in `docs/public/reference/nornicdb-pitfalls.md`:

- a bare top-level UNION drops every row when its first branch is empty
  (allInfraLabels starts with the frequently-empty CloudResource);
- outer aggregation over a CALL subquery (`CALL { ... } RETURN groupExpr,
  count(n)`) evaluates the group key to null and collapses all rows into one
  bogus bucket. The fix groups inside each branch and merges the passed-through
  rows in Go.

`QP-INFRA-RESOURCE-AGGREGATE` is registered in the `go/internal/queryplan`
static gate (forbidden operator: AllNodesScan).

Observability Evidence: no new metric, span, or log field. The aggregate store
runs through the existing `GraphQuery.Run` span and query-duration
instrumentation; the number of UNION branches does not change the surfaced
telemetry.

Note on the live API round-trip: the throwaway stack's single-use MFA recovery
credential blocked an authenticated `GET /api/v0/infra/resources/count` browser
call. The store is instead proven by (a) the focused Go tests exercising
`CountInfraResources`/`fillBuckets`/`InfraResourceInventory` with a stub graph,
(b) the exact generated Cypher's live exact-equivalence above, and (c) the
unchanged HTTP handler wiring. Re-run the authenticated endpoint on a stack with
a fresh credential to add the browser rung.

## #5280 — code-flow read: repo-anchored partial index

Performance Evidence: `query.listActiveCodeFlowFactsSQL` (serving the code-flow
reads) filters `fact_kind IN (code-flow kinds) AND payload->>'repo_id' = $repo`.
Without a matching index the planner satisfies the fact_kind + scope_id join via
`fact_records_scope_generation_idx` but leaves `payload->>'repo_id'` a residual
heap filter, so it fetches every code-flow fact in every scope and discards the
non-target rows — work linear in total corpus code-flow volume, not the target
repo. This was originally mis-filed as a sequential scan and initially assessed
as not-a-defect on a zero-taint-fact corpus; an adversarial re-validation with
seeded volume proved the residual-filter over-fetch is real.

Seeded 83,000 synthetic code-flow facts (`code_taint_evidence` /
`code_interproc_evidence`) across all 41 active scopes (hot repo = 3,000, others
= 2,000), `ANALYZE`, then `EXPLAIN (ANALYZE, BUFFERS)` of the exact query for a
single small repo (2,000 of the 83,000 facts):

| | Execution time | Buffers | repo_id predicate | Over-fetch |
|---|---:|---:|---|---|
| Before (no index) | 33–38 ms | 29,889 shared | residual `Filter` | `Rows Removed by Filter: 1976 × 41 loops` |
| After (`fact_records_code_flow_repo_idx`) | 4.9 ms (small) / 4.5 ms (hot) | 2,296 shared | in the `Index Cond` | none |

~6.8× latency, ~13× buffers, and the planner adopted the index immediately.

Index application proof (Index Doctrine — populated store):
- first `CREATE INDEX IF NOT EXISTS` on the 413k-row populated table: created,
  query switches to `Index Scan using fact_records_code_flow_repo_idx`;
- reapply (schema re-bootstrap): no-op, index oid stable (97170 → 97170);
- `docker compose restart postgres`: index persists, query still uses it.

The partial predicate deliberately omits `is_tombstone`: the read's
`ranked_candidates` CTE must rank retracted facts alongside live ones to pick
the newest generation per `stable_fact_key` before dropping `rn=1` tombstones —
excluding tombstones from the index would resurface deleted code-flow evidence.
The `.go` schema const and `migrations/003_fact_records.sql` carry the
byte-identical statement (lockstep test `TestFactRecordSchemaIncludesCodeFlowRepoIndex`).

All 83,000 seeded facts and the manual index were removed after measurement
(`VACUUM ANALYZE`; verified `fact_records` = 330,474, 0 leftover).

Observability Evidence: no new metric, span, or log field. The code-flow read
keeps its existing Postgres query span and duration/error instrumentation; the
index only changes the chosen plan, which stays visible through the same span
and `EXPLAIN`.

## #5278 — full-corpus performance proof (deferred P2)

No-Regression Evidence: the merged #5278 infra/resources/search fix was proven
at 3,178 infra nodes; the deferred item was a larger-scale wall-clock proof.
Re-measured on this 91,335-node corpus (query `terraform`), old whole-graph
shape vs the merged per-label `CALL` shape:

| | Cold | Warm | Rows | Exactness |
|---|---:|---:|---:|---|
| Old (`MATCH (n) WHERE OR-of-labels`) | 752.9 ms | 1.4 ms | 3 | — |
| New (merged #5278 per-label CALL) | 82.1 ms | 1.7 ms | 3 | 0/0 vs old |

9.2× cold win at 91k nodes (656 ms → 8 ms was the 6.5k-node figure), exact-equal
results — confirming the fix scales with the infra-label population rather than
total graph size.
