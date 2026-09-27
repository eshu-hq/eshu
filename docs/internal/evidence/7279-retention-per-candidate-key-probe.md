# #7279: generation retention probes candidate keys instead of scanning fact_records

Generation retention runs one transaction per batch. The candidate statement
locks up to 100 `ingestion_scopes` rows `FOR UPDATE`, and those locks are held
through the row count, the three content prunes, and the commit. A fact insert
into a locked scope takes the foreign key's `FOR KEY SHARE` lock on that scope
row, so it waits for the whole transaction.

Since #6809 the row count and the three prunes each decided "is this content
key still held by a retained generation?" with a grouped pass: one aggregate
over every live fact of a kind (`content_entity` or `file`). That pass costs
O(`fact_records`), and it ran up to four times per cycle while the scope locks
were held. On ops-qa `fact_records` holds 93.9M rows in a 115 GB heap; a plain
`EXPLAIN` of the entity prune there shows a `Seq Scan on fact_records`
estimated at 48.8M rows feeding a 64-partition `HashAggregate`.

## The change

- **Two partial key indexes**, one `CREATE INDEX CONCURRENTLY` per migration
  file: `139_fact_records_content_entity_key_idx.sql` on
  `((payload->>'repo_id'), (payload->>'entity_id'))` and
  `140_fact_records_file_key_idx.sql` on
  `((payload->>'repo_id'), (payload->>'relative_path'))`, each
  `WHERE fact_kind = '<kind>' AND is_tombstone = FALSE`. `generation_id` is
  left out: `generation_id <> ALL($1)` is not btree-indexable, and without it
  deduplication folds a key's repeats across generations (91 MB against 407 MB
  on the perf fixture).
- **Per-candidate SQL** in `generation_retention_sql.go`. Candidate keys are
  read from the candidates' own facts through `scope_generations` and the
  `(scope_id, generation_id)` prefix of `fact_records_scope_generation_idx`. A
  key is doomed when a `NOT EXISTS` probe of the key index finds no live fact
  outside `$1`. The row count's `fact_records` arm is scope-joined the same way.
  Everything else is unchanged: the candidate statement, the lock set and
  order, the caps, `SET LOCAL work_mem`, and the `inventory` statements.
- **A precondition, not a fallback.** Without a key index every probe scans
  `fact_records` once per candidate key (the #6809 cliff), so
  `PruneSupersededGenerations` checks the catalog first
  (`generationRetentionKeyIndexesQuery`: valid, ready, non-unique, two-column
  btree, exact expressions and predicate). If either index fails, the cycle is
  refused before any lock is taken, with `ErrGenerationRetentionKeyIndexUnavailable`.
- **Cold bootstrap builds them too.** They are not added to
  `BootstrapDefinitionsWithoutContentSearchIndexes`: nothing rebuilds a deferred
  `fact_records` index later, retention would refuse until one existed, and a
  btree over two kinds has no measurable insert cost on the fixture (below).

Migration numbers 139 and 140 are the next free after `origin/main` merged
#7206 (`138_content_file_secret_lines.sql`); this branch originally claimed
138 and 139 and was renumbered on rebase. Any other open PR still adding
migrations from 138 upward must renumber past these two and re-pin the
manifest and golden digest.

## Hypothesis ledger

| candidate | cheapest proof | old | new | accuracy | concurrency | disposition |
| --- | --- | ---: | ---: | --- | --- | --- |
| per-candidate key probe, scope-joined (v3) | perf fixture, then this PR's live tests | grouped pass, O(table) | O(batch) | identical counts and survivors | same locks, shorter hold | proven |
| per-candidate probe, generation-only enumeration (v2) | cold-statistics `EXPLAIN` | - | Parallel Seq Scan cold | exact | - | rejected |
| lock-scope reduction, scans outside the lock | analysis | - | - | opens a lost-content-row race | - | rejected |
| three-column key index (with `generation_id`) | perf fixture | 407 MB | 91 MB (two-column) | same | same | rejected on size |

## Measurements

Performance Evidence: laptop-local (Apple Silicon, `postgres:18-alpine`
PostgreSQL 18.6, `shared_buffers` 512MB, host load high, so shared buffers are
the stable figure and wall times are noisy), from
`TestGenerationRetentionProbeCostIsIndependentOfTableSizeLive`. One 12-generation
candidate batch (4 scopes, 1,000 entity and 200 file facts per generation) is
measured at 1x bystanders (16 scopes) and again, with an identical fresh batch,
after tripling them (48 scopes, about 820k facts). Figures are `fact_records`
scan-node shared buffers, with the whole statement's execution time; the
grouped column runs the frozen #6809 statements on the same fixture in the same
run.

| statement | probe 1x -> 3x | grouped 1x -> 3x |
|---|---|---|
| row count | 37,946 -> 38,570 (1.016x), 42.8 -> 37.1 ms | 29,690 -> 83,191 (2.802x), 212.7 -> 917.0 ms |
| references prune | 10,984 -> 6,065 (0.552x), 10.8 -> 7.8 ms | 2,376 -> 5,471 (2.303x), 12.6 -> 37.8 ms |
| entities prune | 30,137 -> 30,136 (1.000x), 35.2 -> 46.9 ms | 13,657 -> 38,860 (2.845x), 145.5 -> 480.3 ms |
| files prune | 6,064 -> 6,065 (1.000x), 7.0 -> 6.8 ms | 2,376 -> 5,471 (2.303x), 11.5 -> 32.2 ms |

- The probe's work follows the batch; the grouped pass follows the table. The
  test fails if a probe statement grows more than 10%, and it fails if the
  grouped control grows less than 1.5x, so a fixture that cannot show growth
  cannot pass.
- The references prune fell at 3x because at 1x the planner joined
  `content_file_references` before the anti-join and probed once per reference
  row (two per key); both are bounded by the batch.
- At 1x the probe reads more buffers than the grouped pass: this batch is about
  12% of the fixture's facts, above the roughly 10% crossover the perf
  diagnosis measured. On ops-qa the first backlog batch is about 300k of 47M
  entity facts (0.6%).
- A run where the bystanders straddled a btree level boundary showed the entity
  probe rising from 5.6 to 6.6 buffers per key: a log-scale step, not linear
  growth. The fixture now sizes bystanders so both points share a depth.

Performance Evidence: scope-lock hold and a concurrent FK insert, same test.
Each run is a real `PruneSupersededGenerations` cycle; once its locks are held
a second session inserts a fact into a locked scope. The hold includes a
deliberate 100 ms pause that lets the insert reach its lock wait.

| code | 1x hold / insert wait | 3x hold / insert wait |
|---|---|---|
| origin/main (grouped), RED run | 554 ms / 560 ms | 1,313 ms / 1,316 ms |
| #7279 probe | 472 ms / 486 ms | 459 ms / 478 ms |

The insert completes only after the retention commit begins (asserted), so the
scope lock still blocks it; only its duration changed. In the probe runs the
largest phase is `delete_scope_generations` (FK cascades, 182-237 ms), then
`count_rows` (148 ms); both are candidate-bounded. That cascade is the next
measured long pole inside the lock window.

The larger-scale numbers come from the perf diagnosis on an 11 GB heap (10.8M
rows, 605 scopes): lock hold per cycle 18.7 s and 33.5 s on the grouped shape
against 1.0-1.2 s on the probe; the concurrent FK insert waited 17.8 s and
32.5 s against 0.26 s and 0.012 s; the entity prune read 431,800 buffers at both
3.6M and 10.8M rows. Those were measured by the diagnosis, not re-run here.

| Metric | Before | After | Delta | Evidence |
| --- | ---: | ---: | ---: | --- |
| entity prune `fact_records` buffers, 3x / 1x | 2.845x | 1.000x | growth removed | scale test |
| row count `fact_records` buffers, 3x / 1x | 2.802x | 1.016x | growth removed | scale test |
| scope-lock hold at 3x (laptop fixture) | 1,313 ms | 459 ms | -854 ms | contention test |
| correctness diff | - | 0 rows in 4 batches x 13 tables, 0 survivor diffs | none | exactness test |
| next long pole in the lock window | - | `delete_scope_generations` 182-237 ms | - | phase log |

Classification: phase wall-clock win for the retention lock window; the
ops-qa wall-clock effect is not measured (see Limits).

## Plan stability

`TestGenerationRetentionProbePlansStayOnKeyIndexesLive` walks `EXPLAIN (FORMAT
JSON)` for the row count and the three prunes under three conditions: no
planner statistics (autovacuum off, never analyzed), a forced generic plan on
the cold database, and a forced generic plan after `ANALYZE`. It fails on a
`Seq Scan` of `fact_records`, on any `fact_records` index outside the two
`(scope_id, generation_id)` indexes and the two key indexes, on a key index read
without the key, on a `(scope_id, generation_id)` index read without both
columns (a skip scan over every scope), and when a statement never probes its
key index. On origin/main it failed every statement in every condition (a
`Seq Scan` in the row count, skip scans over
`fact_records_collector_status_active_idx` in all four). With the change all
pass. A seeded subtest drops `fact_records_content_entity_key_idx` inside a
rolled-back transaction and requires the guard to reject the entity prune's
plan.

The `inventory` repository statements (`LockRepositoriesForGenerations`,
`DeleteOrphanedRows`) still read `generation_id = ANY($1)`. Under the same
conditions plus an analyzed custom plan, their plans are index skip scans over
the `(scope_id, generation_id)` indexes, with no `Seq Scan`; the diagnosis
measured them on ops-qa at 18k buffers and 667 index searches. Per the
diagnosis they stay unchanged unless a plan guard shows a sequential scan.

## Exactness

`TestGenerationRetentionProbeMatchesGroupedPassLive` runs the probe and the
frozen #6809 statements side by side on one migrated schema, for the full batch
oldest first, reversed, a six-generation recount subset, and one generation. The
per-(generation, table) row counts are identical for all 13 tables, and the rows
left in `content_entities`, `content_files`, `content_file_references` and
`infra_resource_entities` after the prunes and the orphan delete are identical.
The corpus carries a cross-scope holder, retained tombstones, duplicate
candidate facts for one key, empty and missing key fields, and keys shared by
several candidates. The existing #6809 tests (hand-written expected sets, the
10 s cold-statistics prune test, attribution, work_mem, recount and skip-cap)
pass unchanged.

## The scope-join invariant

The candidate side joins facts to their generation on `scope_id`, so it relies
on every fact carrying its generation's scope. `fact_records` has separate
foreign keys on `scope_id` and `generation_id`, so the schema does not enforce
the pair. The writer audit:

- The collector path (`IngestionStore.CommitScopeGeneration` ->
  `upsertStreamingFacts`) refuses any envelope whose scope or generation differs
  from the generation being committed, and `scope.ScopeGeneration` validation
  ties that generation to its scope.
  `TestStreamingFactWriterRefusesMismatchedScopeGenerationPair` pins it; turning
  off the scope check makes it fail.
- The 18 reducer fact writers (`factwrite.Row`, `factwrite.VersionedRow`,
  `factwrite.SingleInsertQuery` callers) take `ScopeID` and `GenerationID` from
  one source. Most use the intent value; `packages/correlation` reads both from
  one payload; `securityalert` prefers the provider alert fact's own envelope
  pair and falls back per field to the intent, and a stored fact always carries
  both fields, so that fallback does not mix sources in practice.
- No writer's `ON CONFLICT` rewrites a fact's `scope_id` or `generation_id`.
- One theoretical source remains: `upsertScopeGenerationQuery` rewrites a
  *pending* generation's `scope_id` on a generation-ID conflict, which would
  strand facts already written under the old scope. Generation IDs are derived
  per scope, so this needs a cross-scope generation-ID collision.

A violation is bounded and never over-deletes.
`TestGenerationRetentionScopeMismatchedFactNeverOverDeletesLive` pins both
directions: a key named only by a mismatched candidate fact keeps its content
row (a leak), and the `fact_records` count omits that fact although the FK
cascade still removes it, so `BatchRowLimit` is weaker by the number of
mismatched facts. A mismatched fact in a kept generation still protects its key,
because the retained probe matches on `generation_id` only. The perf diagnosis
found 0 mismatches in 1,559,239 ops-qa facts across the 300 generations it
sampled.

## Write cost

The perf fixture could not tell index maintenance from host noise: 60k-row
inserts took 2.90 / 2.38 / 3.72 s with the two indexes and 2.61 / 3.74 / 3.48 s
without, at load average 20-37. ops-qa `fact_records` already carries 105
indexes, and these two apply only to `content_entity` and `file` rows. The
projected ops-qa sizes are 1-2 GB for the entity index and 0.1-0.2 GB for the
file index (a projection from key lengths, not a measurement).

## Refusal

`TestGenerationRetentionRefusesWithoutValidKeyIndexLive` refuses the cycle for a
dropped file key index, an invalid one (a unique concurrent build that failed on
duplicate keys, leaving `indisvalid = false`), and a valid index of the right
name but a one-column shape. Each refusal leaves `scope_generations` unchanged
and no scope row locked; restoring the index lets the next cycle prune all four
generations. The fake-backed unit tests pin that the check runs after the
`work_mem` setting and before the candidate lock, and that a refused cycle
issues no lock, count, or delete.

## Limits

- Not measured on ops-qa: the probe half of the statements (it needs the index),
  the concurrent build time on a 115 GB heap (it reads the heap about twice and
  waits out old snapshots), and the real index sizes. Take a quiet-window
  `EXPLAIN (ANALYZE, BUFFERS)` of the four statements after the indexes exist.
- Until migrations 139 and 140 finish, retention refuses every cycle. The
  migration coordinator builds them at startup; a failed build fails bootstrap
  and is retried after dropping the invalid index.
- The scale and contention test is too heavy for the reducer contention gate's
  300 s budget and is classified scheduled; the exactness, plan, refusal and
  scope-mismatch proofs run in that gate.

Observability Evidence: the runner now records
`eshu_dp_generation_retention_phase_duration_seconds{phase}` for each bounded
phase (`key_index_check`, `select_candidates`, `count_rows`, `record_events`,
`delete_shared_projection_intents`, `prune_content_file_references`,
`lock_infra_repositories`, `prune_content_entities`, `delete_infra_orphans`,
`prune_content_files`, `delete_scope_generations`, `commit`) and
`eshu_dp_generation_retention_scope_lock_hold_seconds`, and logs
`scope_lock_hold_seconds` and `phase_seconds` on "generation retention cycle
completed". A refused cycle increments
`eshu_dp_generation_retention_failures_total{reason="key_index_unavailable"}`
and logs "generation retention cycle refused" with
`failure_class=generation_retention_key_index_unavailable` and the index name.
`TestGenerationRetentionRunnerLabelsKeyIndexRefusal` and
`TestGenerationRetentionRunnerRecordsPhasesAndScopeLockHold` pin both. An
operator seeing FK-insert stalls on a scope can read the lock-hold histogram and
the phase split directly instead of inferring them from
`eshu_dp_generation_retention_duration_seconds`.
