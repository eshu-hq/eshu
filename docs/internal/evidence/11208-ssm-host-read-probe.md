# #11208: SSM host inventory read, per-scope probe cost

This note records the `EXPLAIN (ANALYZE, BUFFERS)` of the SSM host fact read
(`buildSSMHostActiveFactsSQL`, `go/internal/query/ssm_host_inventory_facts.go`)
and what it settles for [#11208](https://github.com/eshu-hq/eshu/issues/11208),
part of #11170 and epic #10860. It changes no runtime code and adds no index.

**Outcome:** the arbiter ruling
`docs/internal/arbiter/11208-ssm-index-ruling-2026-11-22.md` accepts the cost as a
measured cost and rejects the index. The named budget row keeps its 1500 ms
ceiling and now cites the measured slope. Reopen if an empty answer on this
route passes 150 ms p95, or a deployment passes 30,000 active scopes. Those two
thresholds are the arbiter's, derived from the measured slope, not from a
contract.

## Theory and decision

**Theory (from the #11170 gate):** with no SSM facts the read still costs about
3 Postgres buffers per active scope, because the plan probes
`fact_records_scope_generation_idx` once per scope.

**Decision this settles:** is the per-scope probe the cause, and can the read be
bounded without a new `fact_records` index? A new index needed an arbiter
ruling, so the work stopped at these numbers and the ruling above decided it.

## Result

| Question | Answer |
| --- | --- |
| Is the per-scope probe the cause? | Yes. 2,400 of 2,431 buffers are the 800 index searches, 3.0 per scope. |
| Does it grow with scopes? | Yes, linearly: 2,431 buffers at 800 scopes, 9,715 at 3,200. |
| Can a query rewrite bound it with the existing indexes? | No rewrite found that is both exact and bounded. See Rejected rewrite. |
| What makes the empty read constant? | Only a new partial index on the two SSM fact kinds: 4 buffers at both sizes. Not added. |
| Does that index help a read that finds SSM facts? | No. 62,069 buffers without it, 61,254 with it (documented-commands run). |

## Method

- Database: disposable `postgres:18` container, PostgreSQL 18.6 on aarch64,
  `shared_buffers=256MB`, `max_parallel_workers_per_gather=0`. Removed after the
  runs.
- Query: the output of `buildSSMHostActiveFactsSQL` for
  `ssmHostInventoryFilter{AllScopes: true}` at `origin/main` `9f22ee51e4`,
  dumped by a throwaway Go test. Only `$1` became 6001 and `$2` became 11, the
  values `ssmHostInventoryMaxFactRowsPerHost+1` and
  `ssmHostPageBounds(...)` give for the default limit.
- Fixture DDL: `scripts/dev/11208-ssm-probe/seed.sql`. It is hand-written and has
  only the tables and indexes the read can use. It is not the production schema.
- Each figure is `EXPLAIN (ANALYZE, BUFFERS)`, one warm-up run discarded, then
  three runs. Buffer counts matched on all three runs of every cell. All reads
  were `shared hit`, none `read`.
- Every figure below comes from the committed scripts. An earlier pass with a
  hand-copied query gave the same buffer counts.
- **The probe SQL is the exact output of `buildSSMHostActiveFactsSQL` (AllScopes,
  default limit).** A throwaway Go test (deleted) emitted
  `buildSSMHostActiveFactsSQL(ssmHostInventoryFilter{AllScopes: true})` at
  `9f22ee51e4`, with bind args `[]interface{}{6001, 11}`. `probe_base.sql`
  equals that text byte for byte after `$1` and `$2` are replaced by those two
  values. The hand-copied first-pass query equals it once whitespace is removed.

```text
# The container name must start with perf11208-: measure.sh refuses any other.
docker run -d --name perf11208-pg -e POSTGRES_PASSWORD=<pw> -p 127.0.0.1:<port>:5432 \
  postgres:18 -c shared_buffers=256MB -c max_parallel_workers_per_gather=0
scripts/dev/11208-ssm-probe/measure.sh perf11208-pg seed 800 169600 100
scripts/dev/11208-ssm-probe/measure.sh perf11208-pg explain scripts/dev/11208-ssm-probe/probe_base.sql
# 3,200 scopes: seed 3200 678400 400. Non-empty: seed 800 169600 100, then ssm.
```

## Fixture

| Run | Scopes | AWS scopes | Generations | Fact rows | SSM fact rows |
| --- | --- | --- | --- | --- | --- |
| Empty, small | 800 | 100 | 1,120 (800 active) | 176,384 | 0 |
| Empty, large | 3,200 | 400 | 4,480 (3,200 active) | 705,536 | 0 |
| Non-empty | 800 | 100 | 1,120 (800 active) | 260,664 | 84,280 |

Arguments: small `seed 800 169600 100`, large `seed 3200 678400 400`, and
non-empty `seed 800 169600 100` then `ssm`. The non-empty total is the 176,384
seeded rows plus the 84,280 SSM rows. The 800-scope fixture matches the gate's default corpus in scope count and
active generations. Its fact mix is synthetic (seven kinds, about 212 per
scope), not the gate's 150,000 IaC and 20,000 finding facts.

## Empty answer, existing indexes

| Scopes | Buffers (3 of 3 runs) | Execution | Plan |
| --- | --- | --- | --- |
| 800 | 2,431 | 4.1 to 4.7 ms | Nested Loop, `Index Searches: 800` on `fact_records_scope_generation_idx` |
| 3,200 | 9,715 | 15.9 to 17.8 ms | Nested Loop, `Index Searches: 3200` on the same index |

Per scope: 2,400 / 800 = 3.0 buffers (consistent with a three-page btree descent; the levels were not inspected), and
9,600 / 3,200 = 3.0. The other 31 and 115 buffers are the two small scope
tables, which also grow with scope count. Both runs return zero rows.
Millisecond figures come from a loaded shared host (load 45 to 57) and are not
budget evidence. The buffer counts do not depend on host load.

The issue's seeded-gate read (2,454 buffers, 2,406 in the probes, 802 index
searches, 2.355 ms) agrees with the 800-scope row here within the fixture
difference.

## Rejected rewrite: scope collector prefilter (C1)

C1 adds `scope.collector_kind = 'aws'` so only AWS scopes are probed.

| Scopes (AWS) | Buffers | Execution |
| --- | --- | --- |
| 800 (100) | 331 | 1.0 to 1.1 ms |
| 3,200 (400) | 1,315 | 3.6 to 4.4 ms |

It is rejected for two reasons:

- **Accuracy.** Nothing in this repository ties the two SSM fact kinds to a
  scope with `collector_kind = 'aws'`. The fact-kind registry and the admission
  hook do not pin a kind to a collector kind. The satellite manifest lists
  `aws`, but nothing enforces it for every producer. A scope that carries these
  facts under another collector kind would silently vanish from the read. An
  equality proof is not available here.
- **It is still linear.** It scans AWS scopes, so an install with many AWS
  scopes and no SSM facts pays about 3 buffers for each. It trades a constant
  factor for an unproven predicate.

## Candidate for a ruling: partial index (not added)

```sql
CREATE INDEX perf11208_ssm_idx ON fact_records (scope_id, generation_id)
    WHERE fact_kind IN ('aws_ssm_host_application', 'aws_ssm_host_platform');
```

| Run | Buffers | Execution | Plan |
| --- | --- | --- | --- |
| 800 scopes, empty | 4 | 0.08 to 0.09 ms | `Index Searches: 1` on the new index |
| 3,200 scopes, empty | 4 | 0.08 to 0.10 ms | `Index Searches: 1` on the new index |
| 800 scopes, 84,280 SSM rows | 61,254 | 215 to 308 ms | `Index Searches: 800` on the new index |

The planner most likely picks one search because the empty index has a zero
row estimate (not isolated). With SSM facts present it goes back to one search per scope. Size:
8 kB empty, 616 kB at 84,280 SSM rows. Writes of the two SSM kinds (inserts,
updates, deletes and the `ON DELETE CASCADE` path) maintain it. Its write cost
was not measured.

### Proof limits for the index numbers

- Synthetic fixture and hand-written DDL, one aarch64 host, warm cache only. No
  cold `read` was measured.
- Insert and write amplification, vacuum cost, and migration time on a large
  `fact_records` were not measured.
- The plan with the production schema's other partial indexes present was not
  measured.
- There is no extrapolation here beyond 3,200 scopes. About 3 buffers per scope
  predicts roughly 30,000 buffers at 10,000 scopes. That is a prediction, not a
  measurement.

## Separate observation: the non-empty read

With SSM facts present (800 scopes, 84,280 SSM rows, 20 AWS scopes with 10
hosts each) the read costs 62,069 buffers, with `temp read=492 written=1540`.
The window functions sort all active SSM rows before the host rank cap applies.
This is outside #11208, which is about the empty read, and is not proven to
matter at production data sizes. It is recorded so a later measurement of the
non-empty route has a baseline.

| Run (documented commands) | Buffers | Execution |
| --- | --- | --- |
| Existing indexes | 62,069 | 280.2 to 430.1 ms |
| Partial index added | 61,254 | 214.9 to 308.0 ms |
| C1 prefilter | 59,969 | 224.0 to 364.6 ms |

The first pass of this work measured the same read on a seed with 176,800
non-SSM rows (`seed 800 170000 100`) and got 62,069, 61,255 and 59,969 buffers
and 187 to 240 ms. The two passes agree within one buffer. The milliseconds
differ because the host load differed, which is why they are not budget
evidence. The arbiter ruling quotes the first pass.

## What is not checked

- NOT_CHECKED: the seeded gate stack itself. These runs use a synthetic fixture.
- NOT_CHECKED: a cold-cache read and a concurrent-reader run.
- NOT_CHECKED: the effect on the #11170 gate row. The named row keeps its 1500 ms
  ceiling (`testdata/benchmarks/read-api-route-budgets.txt`); only its comment
  changed, per the ruling.

## Scripts

`scripts/dev/11208-ssm-probe/` holds the fixture, the probe SQL, and
`measure.sh`. They are measurement shims, not gates. `measure.sh` refuses a
container name that does not match `^perf11208-`, proved by
`test-measure-guard.sh` (RED with the guard removed, GREEN with it). `seed.sql`
aborts, before it drops anything, when `ingestion_scopes` has rows and is not
marked `perf11208 fixture`. That guard was checked live on a disposable
container: a plain `ingestion_scopes` with one row made the seed exit 3 and the
row survived, and an empty table or a marked fixture was replaced.
