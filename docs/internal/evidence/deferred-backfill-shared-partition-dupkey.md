# Deferred backfill shared-partition duplicate-conflict-key fix

## Bug

`writeDeferredBackfillBatch` (`go/internal/storage/postgres/ingestion_backfill.go`)
loops per source repository and appends one `reducer.GraphProjectionPhaseState`
and one `scopeGenerationPartition` memo candidate per repo. Both sinks' conflict
targets — `graph_projection_phase_state`'s six-column primary key and
`deferred_backfill_partition_memo`'s `(scope_id, generation_id)` primary key —
are functions of `(repoGeneration.ScopeID, repoGeneration.GenerationID)` alone.
When N repositories in one batch share a `(scope, generation)` partition (the
ingestion commit path accepts multi-repo scopes; production git sync just
happens to commit one repo per scope, so this had not fired there), the batch
upsert carried N byte-identical conflict keys in one
`INSERT ... ON CONFLICT DO UPDATE` and Postgres rejected the whole batch
transaction with `SQLSTATE 21000` ("ON CONFLICT DO UPDATE command cannot affect
row a second time"). A live Ifá gate run observed this exact failure:
`publish backward evidence readiness: upsert graph projection phase state batch
(5 rows)`. The transaction rollback meant no repository in the batch got its
readiness published, including well-formed repositories sharing the batch with
no bug of their own.

## Fix

Readiness and the partition memo are `(scope, generation)` facts, not per-repo
facts. `writeDeferredBackfillBatch` now dedupes the partition once per batch
with a `publishedPartitions` seen-map, feeding both `phaseRows` and
`memoCandidates` from the same dedupe. The per-repo relationship-evidence
upsert above it is unchanged — evidence genuinely is per-repo.

## No-Regression Evidence

The dedupe strictly reduces the row count fed into both batched upserts per
batch (at most one row per distinct `(scope, generation)` partition instead of
one row per repository), so it cannot regress row-set correctness — and the
pre-fix behavior being compared against is not "more rows successfully
written" but a hard transaction ABORT that published zero readiness rows for
the whole batch, so the comparison is against complete failure, not a smaller
success.

Failing regression test first (`go/internal/storage/postgres/ingestion_backfill_shared_partition_dupkey_test.go`,
`TestWriteDeferredBackfillBatchSharedScopeGenerationDedupesConflictKey`),
driven through the real `IngestionStore.CommitScopeGeneration` ->
`writeDeferredBackfillBatch` path against a throwaway `postgres:18-alpine`
container (`ESHU_POSTGRES_DSN`, matching the sibling
`TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence`
proof's DSN convention): two repositories (`repo-alpha`, `repo-beta`) committed
under one scope+generation (`scope-shared`/`gen-shared`), then
`writeDeferredBackfillBatch` called with both repo IDs in one batch.

Before the fix:

```
writeDeferredBackfillBatch() error = publish backward evidence readiness:
upsert graph projection phase state batch (2 rows): ERROR: ON CONFLICT DO
UPDATE command cannot affect row a second time (SQLSTATE 21000) (SQLSTATE
21000), want nil
--- FAIL: TestWriteDeferredBackfillBatchSharedScopeGenerationDedupesConflictKey (1.11s)
```

After the fix, the same test asserts the batch commits with EXACTLY one
readiness row and one memo row for the shared partition (not merely "no
error"):

```
--- PASS: TestWriteDeferredBackfillBatchSharedScopeGenerationDedupesConflictKey (0.89s)
```

A second test, `TestWriteDeferredBackfillBatchDistinctScopesPublishOneRowEach`,
proves the dedupe does not over-collapse: two repositories in two DISTINCT
scope+generation partitions (the ordinary one-repo-per-scope shape) still
publish one phase row and one memo row EACH (`published == 2`); it passed both
before and after the fix, since it never exercised the buggy shared-key path.

Full package regression: `go test ./internal/storage/postgres -count=1`
(hermetic subset, no DSN) and
`ESHU_POSTGRES_DSN=<throwaway postgres:18-alpine DSN> go test
./internal/storage/postgres -count=1` (full DSN-gated live subset) both pass
except one pre-existing, unrelated failure
(`TestReducerClaimFencedSiblingBecomesClaimableAfterAck`, a reducer
conflict-claim proof referencing a `fact_work_items` column the throwaway
schema helper does not create) reproduced byte-identically on unmodified
`origin/main` against the same container, proving it predates this change.
The sibling live proof
`TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence`
(the #4444 derived-evidence fencing regression guard, same package, same DSN
convention) and the existing generation-consistency guard
(`TestWriteDeferredBackfillSkipsReadinessWhenGenerationAdvanced`) and
concurrency proofs (`TestWriteDeferredBackfillInBatchesRunsConcurrently`,
`TestWriteDeferredBackfillInBatchesSerialWhenWorkerCountOne`) all continue to
pass unchanged.

## No-Observability-Change

The fix adds no metric, span, log key, route, worker, lease, queue domain, or
runtime knob. `writeDeferredBackfillBatch`'s return value (`published`) already
documented itself as "the number of readiness rows published" — it now
correctly reports one row per distinct partition instead of double-counting
repositories that share a partition; the one caller of that count
(`deferred_backfill_completed`/`deferred_backfill_batch_committed` structured
log fields) reads more accurately after the fix, with no new log key or shape.
Operators continue to diagnose this path through the existing
`eshu_dp_postgres_query_duration_seconds` Postgres query-span instrumentation
and the existing `deferred_backfill_batch_committed`/`deferred_backfill_completed`
structured logs.
