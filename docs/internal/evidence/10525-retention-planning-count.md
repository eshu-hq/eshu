# Evidence: #10525 — retention planning count still scans the full candidate set

Scope of this record (PR-1): the `fact_work_items` arm of
`generationRetentionRowCountsQuery`
(`go/internal/storage/postgres/generation_retention_row_counts_sql.go`),
the pre-delete work-item lock `lockPrunedGenerationWorkItemsQuery`
(#8261), and the fail-closed key-index check
`generationRetentionKeyIndexesQuery` (`generation_retention_sql.go`).
The fix adds one index, migration
`294_fact_work_items_generation_idx.sql`: `fact_work_items
(generation_id)` (migrations 167/292 pattern), and wires it into the
key-index check through the same single-column leg as migration 292.

This is a **Prove-The-Theory-First** record. Every shim below ran
read-only on the QA environment's Postgres primary (PostgreSQL 18.3,
`eshu_schema_migrations` head = migration 292) before production
code was written; the one candidate-index measurement ran inside a
transaction that was rolled back (`shim_index_remaining = 0` after).
The live plan guards in
`generation_retention_row_counts_10525_live_test.go` re-prove the plan
shape on every run. Gate 3 bars: locked recount at most 2.0 s warm on
the fitted subset, `ScopeLockHold` at most 5 s on a live cycle;
planning count at least 5x under the 30.5 s warm baseline on identical
boundaries (PR-2, see below).

## What went wrong

The count arm and the pre-delete lock both match `fact_work_items` on
`generation_id` alone, and no index leads with that column: the
scope-generation index leads with `scope_id`. Both statements read the
whole table (288,824 rows on QA) in both statement roles — unlocked as
the planning count and locked inside `ScopeLockHold`.

## Shim results (QA, oldest 100 superseded generations, 83 scopes)

Full shipped statement, forced generic plan, `work_mem = 64MB` as in
the retention transaction: **31.28 s** for the 100-generation batch
(the 30.5 s warm Gate 3 baseline reproduced on identical boundaries).
Per-arm decomposition on the same batch:

| Arm (100 gens unless stated) | Buffers | Time |
|---|---|---|
| `fact_work_items`, current generation-only, generic | 33,976 | 158 ms warm (seq scan + hash) |
| `fact_work_items`, scope-join variant, custom | 1,110 | 3.97 ms (100 searches) |
| `fact_work_items`, behind txn-local `(generation_id)` index, generic | 316 | 1.34 ms (100 searches) |
| `fact_work_items`, fitted 28 gens, behind index, generic | 88 | 0.20 ms (28 searches) |
| Pre-delete lock, current (plan cost) | — | 15,245 (skip-scan) |
| Pre-delete lock, behind index (plan cost) | — | 554 |
| `shared_projection_acceptance`, generation-only audit, generic | 2,378 | 7.9 ms warm |
| `shared_projection_intents` post-292, custom | 1,032 | 249 ms cold-read (83,345 batch rows) |
| `candidate_*` + `doomed_*` CTEs, n=100 | — | 26.26 s (283,425 entity probes, 17.6 s) |
| `candidate_*` + `doomed_*` CTEs, n=50 / n=25 / n=10 | — | 36.3 s / 14.0 s / 31.5 s (cold-to-warm order effects; probes 196k / 88k / 82k) |

Fitted-subset decomposition (oldest-first fit under
`BatchRowLimit = 100,000`: **28 generations, 99,943 rows**, ledger
counts excluded from the fit as an approximation): full statement
**1,139 ms warm** (run 2; run 1 warm-up 1,959 ms), of which
`doomed_entities` 550 ms (21,976 probes), `doomed_files` 210 ms
(1,002 probes), and the `fact_work_items` sequential scan node 112 ms.
Doomed is the largest share of the fitted recount but does not by
itself breach the 2.0 s bar, and progressive counting cannot shrink
the locked role — so PR-1 proceeds and the locked bar stays reachable;
the post-roll QA locked-recount check remains the closing proof,
NOT_CHECKED until it runs. The shims above ran after earlier shims had
warmed the key indexes, so these warm figures are optimistic against
Gate 3's colder 2.28 s fitted reading; they are decomposition evidence,
not a Gate 3 pass.

Planning role: PR-1 removes about 0.2 s of the 31.28 s full-batch
count. The residual is `doomed_*` probe volume scaling with all 100
candidates, which only PR-2's progressive (bounded-window) planning
count addresses. PR-1 alone does **not** meet the 5x planning budget;
that bar is PR-2's, per the rescoped issue.

## Other generation-only arms (audited, only the dominant one fixed)

| Arm | QA rows | Generation access today | Verdict |
|---|---|---|---|
| `fact_work_items` | 288,824 | scope-leading index only | **fixed here (294)** |
| `fact_replay_events` | 2 | seq scan, 0.1 ms in-plan | not dominant |
| `semantic_extraction_jobs` | 0 | scope-leading index | not dominant |
| `shared_projection_acceptance` | 14,658 | scope-leading index, 7.9 ms warm generic | same class, not measured-dominant |
| `graph_projection_phase_state` | 117,654 | has `(generation_id)` index | fine |
| `graph_projection_phase_repair_queue` | 0 | scope-leading PK | not dominant |
| `iac_reachability_rows` | 1,336 | scope-leading indexes | not dominant |
| `generation_commit_fences` | 74 | generation-leading PK | fine |
| `shared_projection_unroutable_intents` | 0 | `(generation_id)` index (167) | fine |
| `security_alert_*` | 0 | generation-leading PKs | fine |
| `shared_projection_intents` | 11.8M | `(generation_id)` index (292) | fixed by #10512 |

## Exactness

No count SQL changes: the arm already counts on `generation_id` alone,
exactly the `scope_generations` cascade envelope, and migration 294
only changes the access path. The differential proves it live: shipped
statement, the same arm derived from the shipped constant and run
alone (`derivedRetentionWorkArmParts`, byte-prefix guarded by
`TestRetentionWorkArmDerivedFromShippedConstant`), and the direct
envelope count agree per generation, including a planted
scope-mismatched work row. The rejected scope-join shape under-counts
that generation by exactly the planted row
(`TestGenerationRetentionWorkCountsMatchCascadeEnvelopeLive`).

## Rejected alternative: scope-join

Measured cheaper than today's scan (3.97 ms custom) with **zero**
scope-mismatched or orphan-generation rows on QA (288,824 rows
checked), so two legs of the triple proof hold today. Still rejected
as the default: a scope-joined count silently under-charges
`BatchRowLimit` the first time a writer emits a mismatched row, while
the index keeps the count on the cascade envelope unconditionally and
also fixes the pre-delete lock, which a count reshape would not.

## Fail-closed check

Without migration 294 both statements scan the table in both roles, so
the store refuses the cycle when the index is missing, INVALID, or
partial-only:
`TestGenerationRetentionRefusesWithoutValidWorkItemsGenerationIndexLive`
covers all three seeded REDs and the post-restore GREEN. The check's
single-column leg is now VALUES-driven over both generation indexes
(292 and 294) with identical per-index predicates; the #10512 refusal
tests re-prove the intents leg unchanged.

## Performance Evidence:

Performance claim: the `fact_work_items` count arm and pre-delete lock
now cost with the candidate batch instead of the table. Proof is
arm-level on identical boundaries (same QA 100-generation batch, same
forced-generic plan shape, buffers and wall above: 33,976 buffers /
158 ms to 316 buffers / 1.34 ms; lock plan cost 15,245 to 554), plus
the live plan guard
`TestGenerationRetentionWorkItemsPlansStayOnGenerationIndexLive`
(custom and forced-generic plans, seeded drop-index REDs on both
statements, executed lock buffers tracking the batch: 3,000 rows,
6,324 buffers in the fixture). The Gate 3 bars' closing proof is the
post-roll QA locked-recount and live-cycle `ScopeLockHold` check,
NOT_CHECKED until it runs; the 5x planning budget is PR-2's and is not
claimed here. No Cypher, graph-write, queue, or lease path is touched.
Work-item inserts gain one small btree entry; `generation_id` is
immutable per row, so updates never move it. Concurrency behavior is
unchanged (no lock, claim, or batching change) and proven live: a
foreign `FOR UPDATE` hold on a retained generation's work row neither
blocks the cycle nor loses its row
(`TestGenerationRetentionWorkIndexCycleIgnoresHeldForeignWorkLockLive`).
`BatchGenerationLimit` 100, `BatchRowLimit` 100k, worker counts, and
lock scope are unchanged — no serialization.

No-Observability-Change: this change adds no metric, span, log key, or
status field. Operators keep diagnosing this path through the existing
retention span, phase durations (`count_rows` covers both count
roles), `ScopeLockHold`, and `ErrGenerationRetentionKeyIndexUnavailable`,
which now also names `fact_work_items_generation_idx` when migration
294 is unusable.
