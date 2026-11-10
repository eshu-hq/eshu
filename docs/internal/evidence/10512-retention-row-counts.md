# Evidence: #10512 — retention row counts scan whole tables under scope locks

Scope: `generationRetentionRowCountsQuery`
(`go/internal/storage/postgres/generation_retention_row_counts_sql.go`),
the `shared_projection_intents` and two package key legs, the intents
reap `deleteSharedProjectionIntentsForGenerationsQuery`
(`generation_retention_sql.go`), and the fail-closed key-index check
`generationRetentionKeyIndexesQuery`. The fix adds one index, migration
`292_shared_projection_intents_generation_idx.sql`:
`shared_projection_intents (generation_id)` (migration-167 pattern),
wires it into the key-index check, and reshapes both package key legs
from fact-join scalar subqueries to direct `(scope_id, generation_id)`
joins on the deployed partition indexes (migrations 182 and 183).

This is a **Prove-The-Theory-First** record. The shim measurements below
(S2/S3) were taken on the QA corpus before the change was written; the
live plan guards in
`generation_retention_row_counts_10512_live_test.go` re-prove the plan
shape on every run. Arbiter ruling budgets: locked recount at most
2.0 s warm on the fitted subset, `ScopeLockHold` at most 5 s.

## What went wrong

- The intents count leg and DELETE match `shared_projection_intents` on
  `generation_id` alone, and no index leads with that column: the
  partial pending index (migration 108) covers only
  `completed_at IS NULL` rows, so a pruned generation's completed
  intents are invisible to it. Both statements sequential-scan the
  whole table.
- The two package key legs counted through the generation's facts
  (`fact_records` probe, then the keys primary key per fact). That
  costs one index probe per fact of the batch and under-counts orphan
  keys (no fact) that the `(scope_id, generation_id)` generation delete
  still removes — the `FactKeyRowsCounted` vs `FactKeyRowsPruned`
  divergence the runner WARNs on, and a `BatchRowLimit` under-charge.

## Shim results (QA corpus, 100-generation batch)

| Shape | Buffers | Time |
|---|---|---|
| Intents count leg, no generation index (11.5M intents) | 2.0M | 56.9 s |
| Package manifest leg, fact-join subquery | 1.15M probes | 5.7 s |
| Package manifest leg, direct `(scope_id, generation_id)` join, custom plan | — | 80 ms |
| Package manifest leg, direct join, generic plan | — | 48 ms |

The direct legs bring the locked recount inside the 2.0 s warm budget;
the intents leg and DELETE become one btree probe per candidate once
migration 292 lands, so their cost follows the batch, not the table.
The entities probe needed no fix (arbiter ruling).

## Exactness

The direct legs read exactly the `factkeys` generation-delete envelope
(`WHERE scope_id = $1 AND generation_id = $2`), so the count charges
every row the delete removes. On clean keys the direct and legacy
fact-join counts agree per generation; planted orphan keys move the
direct count by exactly the planted rows. Both halves are asserted live
in `TestGenerationRetentionPackageKeyCountsMatchFactJoinOnCleanKeysLive`.

## Fail-closed check

Without migration 292 both intents statements scan the table while the
batch holds its scope locks (the #6809 cliff), so the store refuses the
cycle when the index is missing, INVALID, or partial-only:
`TestGenerationRetentionRefusesWithoutValidIntentsGenerationIndexLive`
covers all three seeded REDs and the post-restore GREEN.

## Rejected alternatives

- Keeping the fact-join subqueries: measured 5.7 s for one arm and an
  orphan under-charge — rejected on both counts.
- Widening the migration 108 partial index: it serves the
  pending-intent claim path, a different predicate; the full
  `(generation_id)` btree serves both retention statements at one small
  entry per intent insert. The partial index stays.

## Performance Evidence:

Performance claim: the retention locked recount and intents DELETE cost
now track the candidate batch instead of table size; locked recount at
most 2.0 s warm (fitted subset), `ScopeLockHold` at most 5 s. Proof:
the QA shim table above, plus the live plan guard
`TestGenerationRetentionRowCountPlansStayOnGenerationIndexesLive`
(custom and forced-generic plans, seeded drop-index REDs, executed
DELETE buffers tracking the batch). No Cypher, graph-write, queue, or
lease path is touched. Intents writes gain one small btree entry per
insert; concurrency behavior is unchanged (no lock, claim, or batching
change), and no telemetry changes: the existing retention span, the
`fact_key_count_diverged` WARN, and `ErrGenerationRetentionKeyIndexUnavailable`
already cover the touched surface.

No-Observability-Change: this change adds no metric, span, log key, or
status field. The branch diff adds no telemetry emission line (only
comment updates around the existing WARN); operators keep diagnosing
this path through the existing retention span, the
`fact_key_count_diverged` WARN, and
`ErrGenerationRetentionKeyIndexUnavailable`.
