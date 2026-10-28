# Package-only readiness anchor fence (#8995)

The package-only readiness anchor (`$11 = ''`, `$20 = true`) applied the
target keys after joining, so under a generic plan its cost followed the
scope count instead of the matching keys: one sidecar probe per active scope
per target key. The arm now fences the target keys in a MATERIALIZED probe
of `package_manifest_consumption_keys_lookup_idx` first, then joins
fact/scope/generation per surviving row. No migration: query text only, and
the sidecar indexes are unchanged (lookup index from migration 139).
Rollback: not applicable (no migration; a revert restores the previous leg).

## Measured shape (generic plan, 200,000-row corpus, PostgreSQL 18.6)

Corpus mirrors the issue's shim at production DDL (200 scopes x 1,000
`content_entity` dependency facts, one sidecar key row each, 4 ecosystems x
500 names, every 40th fact a tombstone; 5 target keys matching 2,000 sidecar
rows, 1,800 live facts). `EXPLAIN (ANALYZE, BUFFERS)` under
`force_generic_plan` — the pgx prepared-statement path; the custom plan
already folds the bind values into the probe, so only the generic plan can
catch this class.

| Subject | Before | After |
| --- | --- | --- |
| Anchor leg sidecar probe | Index Only Scan, `ecosystem`+`package_name`+`scope_id`+`generation_id` in Index Cond, **1,005 loops** (201 scopes x 5 keys), 5,015 buffers | Bitmap Heap Scan over Bitmap Index Scan on the lookup index, `ecosystem`+`package_name` in Index Cond, **5 loops** (one per target key), 2,060 buffers; 2,000 fact PK probes follow |
| Anchor leg statement | 11.2 ms, 13,027 shared buffers | 12.3 ms, 13,664 shared buffers |
| Full readiness statement | 1,551.6 ms, 102,482 shared buffers | 1,454.4 ms, 103,119 shared buffers |
| Result rows | 1,800 package.consumption facts | identical (differential diff 0, pinned count 1,800) |

Read the table as a scaling fix, not a local-latency fix: on this
warm-cache, small-payload corpus the anchor leg was already index-served per
probe, so wall time barely moves — but the probe count drops from
scopes x keys to keys (1,005 to 5 at 200 scopes; the before shape grows with
every added scope, the after shape does not). The full statement stays
dominated by the other family CTEs' generic-plan scope probes (~100k shared
buffers both ways); those legs are anchor-independent and out of scope here.
The isolated-leg shim on the same host and shape showed the same mechanism
more starkly (generic plan joining all 195,000 active sidecar rows first,
969,200 rows removed by join filter, 344 ms warm).

A plain join reorder does not hold: the planner emits the same plan, so only
the `MATERIALIZED` fence fixes the shape (prior-session shim proof). The
inner `WHERE $20` keeps the fence free for repository-anchored reads
(`$20 = false` prunes the CTE without touching the sidecar); the
repository-arm scope proof still passes its statement buffer bound.

## Proof

`TestSupplyChainImpactReadinessPackageAnchorLive`
(`go/internal/query/supply/chain/impact/readiness_package_anchor_plan_live_test.go`):
fails before (sidecar probe runs 1,005 loops, want <= 5 target keys),
passes after. It asserts the lookup index with the target keys in the Index
Cond on both the derived anchor statement (derived from the shipped
`ListReadinessQuery`, never hand-copied) and the full statement, plus
row-identity between the rewritten and previous statements over the shared
fixture and an independent count pin.

## Observability

No-Observability-Change: SQL-text-only change returning identical rows (see
differential above); no new stage, metric, span, or log key. The read stays
timed by the existing `eshu_dp_postgres_query_duration_seconds`.
