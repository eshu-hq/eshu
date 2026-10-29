# Owned-package read driven by active generations (#9426)

## What changed at runtime

`FactStore.ListOwnedPackageDependencyTargets`
(`go/internal/storage/postgres/owned_package_targets.go`) reads active Git
dependency declarations for the derived vulnerability-intelligence and
package-registry planners. Its `active_dependencies` CTE now carries one more
predicate:

```sql
AND fact.generation_id = ANY (ARRAY(SELECT active_generation_id FROM ingestion_scopes WHERE active_generation_id IS NOT NULL))
```

The predicate is implied by the existing joins
(`scope.active_generation_id = fact.generation_id` and
`generation.status = 'active'`), so it removes no row they keep. It gives the
planner an indexable restriction on `fact_records.generation_id`, so the read
pays for active-generation rows only. Before, it read every generation's
dependency rows and probed the active-scope index once per row.

- Scope: the same predicate as before, plus the implied generation restriction.
  The hash-slot window, the window stats and the output are unchanged.
- Limit: unchanged (`target_limit + 1` rows per read).
- Timeout: no new statement timeout. The read runs under the coordinator's
  existing context.
- Cardinality: the InitPlan reads one row per scope with an active generation
  (3,314 in the history fixture, 53,314 in the wide fixture). The fact read
  drops from all generations to the active ones.
- Locks: one SELECT, ACCESS SHARE only. The InitPlan and the joins read the
  same snapshot, so a generation that activates during the read cannot appear
  in one and not the other.

## Theory proof (before the change)

The QA read replica was not reachable when this ran: the SSO session had
expired and needs a person to approve the device login. The theory was proven
on a private PostgreSQL 18.6 cluster instead. QA runs the same server version
(`server_version` 18.6-eshu-7957 in the deployed values, and its EXPLAIN output
prints the PostgreSQL 18 `Index Searches` field). The schema came from
migrations 001, 002, 003 and 121 (the four `fact_records` indexes this read can
use) on a seeded fixture that copies the QA shape:

- History fixture: 330 Git scopes with 1 to 40 generations each (mean about
  11, skewed), 936,553 dependency rows in all generations, and 81,236 in active
  generations (8.7%; QA measured 8.6%). Each active generation also holds 20
  non-dependency content entities per dependency. 3,000 non-Git scopes add
  active generations with no dependency rows. 63,199 distinct targets.
- Loser classes: flat history (1 generation per scope, 94% of rows active),
  tiny inventory (5 scopes), a narrow ecosystem filter (`rubygems` only), and a
  wide active-generation array (50,000 more active non-Git generations).

The shipped statement (A) and the candidate (D, A plus the predicate) were
rendered from the Go builder for the vulnerability planner's mode (version
specific, alias expansion, 15 package-manager values, read limit 1,001, target
limit 2,000). The D text is byte-identical to the statement this change ships.
On the fixture, A picks a merge join that sorts all 936,553 rows to disk. QA
picks a nested loop because it estimates 28 rows. So A also ran with
`enable_mergejoin` and `enable_hashjoin` off (A-nl), which reproduces the QA
shape: an index scan of `fact_records_active_package_dependency_entity_idx`
returning every generation's rows, then one active-scope probe per row. D ran
under both settings too (D-nl). Runs were interleaved, the first mover
alternated each round, and the bucket varied per run. The host load average
was 14 to 21 from other work, so wall times are noisy; buffer counts are
deterministic. Figures are EXPLAIN (ANALYZE, BUFFERS) Execution Time in ms.

Performance Evidence: interleaved medians on PostgreSQL 18.6, 2026-10-29:

| Fixture | Variant | n | Median | Min | Max | Buffers (hit + read) |
| --- | --- | --- | --- | --- | --- | --- |
| history | A | 6 | 2,969 | 2,299 | 4,176 | 1,057,679 + 147,311 temp |
| history | A-nl (QA shape) | 6 | 2,112 | 1,176 | 2,957 | 3,803,053 |
| history | D | 6 | 698 | 439 | 940 | 536,272 |
| history | D-nl | 6 | 868 | 441 | 1,263 | 536,272 |
| wide array | A-nl | 4 | 1,223 | 1,061 | 1,434 | 4,029,742 |
| wide array | D | 4 | 413 | 360 | 473 | 374,711 |
| flat | A | 6 | 332 | 292 | 503 | 584,599 |
| flat | D | 6 | 344 | 284 | 415 | 495,766 |
| tiny | A | 6 | 15 | 15 | 16 | 8,255 |
| tiny | D | 6 | 13 | 12 | 14 | 6,840 |

- History: D's median is 33% of the QA-shaped baseline and 24% of the natural
  one, with 7.1 times fewer buffers than A-nl. D's plan is a PostgreSQL 18 skip
  scan of migration 121's `fact_records_content_entity_dependency_variable_repo_idx`
  with `Index Cond: generation_id = ANY (InitPlan 1)` (230 index searches),
  returning the 81,236 active rows. The per-row active-scope probes drop from
  936,553 to 81,236.
- If that index were not used, D falls back to the package-manager index with
  `generation_id = ANY` as an in-index condition. With index 121 dropped inside
  a rolled-back transaction, D read 81,236 heap rows in 638 to 746 ms (about
  416,000 buffers) against A-nl's 1,167 to 1,480 ms (about 4,030,000 buffers).
  The narrow `rubygems` filter takes this same plan on its own: 7.4 ms and
  6,233 buffers against 10 to 14 ms and 34,579 buffers.
- Flat history is the class that can lose, because A already reads only active
  rows. D's median is 12 ms (3.6%) above A's, inside the run-to-run spread
  (D's minimum is below A's), and D uses 15% fewer buffers. The threshold was
  no loser regression above 10%.
- `plan_cache_mode = force_generic_plan` (PREPARE and EXECUTE with the real
  binds) gives the same plan and the same buffers as `force_custom_plan` for
  both A and D on this fixture.

Output equality: for every one of the 64 slots of the history fixture, A and D
returned byte-identical output (62,765 rows, same SHA-1). The same held for the
first 8 buckets of the flat, tiny and wide fixtures. In the repository, the live
subcase below checks the same equality on every slot.

| Candidate | Old | New | Accuracy | Disposition |
| --- | ---: | ---: | --- | --- |
| D: `generation_id = ANY` of active ids, no DDL | 2,112 ms (A-nl) | 698 ms | identical output, all slots | proven (fixture) |
| B: drive from active scopes through `fact_records_collector_status_active_idx` | | | | rejected: reads every active `content_entity` row, about 99% of them non-dependency (migration 121 note) |
| C: new partial index keyed by `generation_id` | | | | not needed: D reaches the threshold on existing indexes |

Classification: diagnostic win plus a component (Postgres read) win on a
fixture. No end-to-end claim. The next long pole for this read is the
coordinator-versus-replica gap below.

## Coordinator versus replica gap (64.6 s against 14 s)

Facts from source:

- The coordinator reads the primary: `cmd/workflow-coordinator/main.go` opens
  `runtimecfg.OpenPostgres`, which opens the writer DSN. The replica figure came
  from the read replica.
- The read uses pgx's default exec mode, which prepares and caches the
  statement per connection. After five executions on one connection,
  PostgreSQL may switch to a generic plan.
- `eshu_dp_postgres_query_duration_seconds` is recorded around `QueryContext`
  through `InstrumentedQueryer`. With a sort under the LIMIT, the first row
  arrives only after all the work, so the figure is the full execution plus any
  wait for a pool connection.

What is ruled in or out:

- Row transfer: ruled out. The read returns at most `target_limit + 1` rows of
  seven short columns.
- JIT: ruled out. The QA plan's total cost estimate is 102.95, far below
  `jit_above_cost` (100,000).
- Generic plan: not reproduced on the fixture (same plan and buffers under
  `force_generic_plan`). NOT_CHECKED on QA statistics.
- Primary versus replica (write contention and a cold cache for history pages):
  plausible, NOT_CHECKED, because the QA cluster was unreachable. D lowers both
  terms, since it reads 7 times fewer buffers on the QA-shaped plan.

## Regression proof in the repository

- `TestOwnedPackageReadDrivesFromActiveGenerations` (hermetic): the predicate
  appears exactly once, inside `active_dependencies`, in all four identity and
  alias modes. The all-history statement is derived by removing it.
- `TestListOwnedPackageDependencyTargetsSlotDifferentialLive`, subcase "read
  follows active generations": it adds eleven superseded generations, a
  pending-only scope and a scope pointing at a non-active generation. Then it
  proves three things. For every slot, the shipped statement's rows equal the
  all-history statement's rows, all eleven columns. Stale versions and
  non-active scopes are absent. The plan's `fact_records` nodes read at most
  2,369 rows (2,359 active, plus the 10 rows of the scope that points at a
  non-active generation), while the all-history plan reads more. Before the
  change the subcase failed: the plan read 2,820 rows.

Observability Evidence: no signal changes. The existing
`owned_package_targets.read` span and its window event keep their attributes.
The read still lands in `eshu_dp_postgres_query_duration_seconds{store="owned_package_targets"}`,
whose 5 to 300 s buckets #9422 added. An operator confirms the change by the
histogram's shift toward the lower buckets after rollout, and on the replica by
the plan above (`Index Cond: generation_id = ANY (InitPlan 1)`).

## NOT_CHECKED

- The QA replica plan, rows, buffers and wall time for D (QA unreachable). The
  QA readback should run the same EXPLAIN on the replica before the rollout
  claim.
- The coordinator-versus-replica gap on QA.
- Cold-cache timing: the fixture ran with a warm OS cache.
