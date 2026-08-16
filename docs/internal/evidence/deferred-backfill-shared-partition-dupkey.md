# Deferred backfill shared-partition duplicate-conflict-key fix

## Bug

`writeDeferredBackfillBatch` (`go/internal/storage/postgres/ingestion_backfill.go`)
looped per source repository and appended one `reducer.GraphProjectionPhaseState`
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

## The wider hazard behind it

Deduping inside the batch removes the `SQLSTATE 21000` abort but leaves a worse
problem the abort had been masking, and it is not new to this change.

Batches are fixed-size contiguous slices of the repo-ID-sorted corpus
(`deferredMaintenanceRepoBatchSize = 32`), committed concurrently by the worker
pool. Repositories sharing a `(scope, generation)` partition are not adjacent in
repo-ID order, so a partition routinely spans several batches. A batch that
publishes readiness and the durable partition memo is therefore claiming the
whole partition's backward evidence is committed on the strength of the subset
that happened to land in it. If a sibling batch carrying the rest of that
partition fails or is canceled, the memo survives the pass, and
`applyDeferredPartitionMemoGate` then skips that partition's fact load on EVERY
later pass until the catalog fingerprint changes. That is durable wrong state,
not a transient failure.

This predates the dedupe fix: pre-fix, a partition with one repository per batch
published exactly the same way. What the dedupe changed is the failure mode of
the multi-repo-per-batch shape, from a loud abort to a silent per-batch publish.

## Fix

Readiness and the partition memo are `(scope, generation)` facts, not per-repo
facts, and they are also not per-BATCH facts. Publication moved out of the batch
into a fan-in step, `publishDeferredBackfillPartitions`
(`go/internal/storage/postgres/ingestion_backfill_pool.go`):

- `writeDeferredBackfillBatch` persists evidence only, and returns the
  `(scope, generation)` partitions it contributed to, mapped to the repositories
  it committed under each. Batch composition, sorting, and contiguity are
  unchanged.
- After `wg.Wait()` returns with no batch error and an uncanceled context, the
  fan-in publishes per partition, in ONE transaction per partition, holding that
  partition's repository advisory locks in the same global sorted order the
  batches use.
- Under those locks it re-reads the scope's active generation
  (`activeScopeGenerationQuery`, the single-scope projection of
  `latestGenerationCTE`) and skips the partition if the generation advanced
  since the batch committed.
- If ANY batch failed or was canceled, the fan-in does not run at all.
- The ArgoCD carve-out in `writeDeferredBackfillPartitionMemos` moved into the
  fan-in transaction unchanged.

Keying publication by partition rather than filtering duplicates out of a
per-repo list makes the duplicate conflict key unrepresentable, so the original
`SQLSTATE 21000` shape cannot recur either.

The partition-aligned-batching alternative was considered and rejected: a
~900-repo scope would become one transaction holding ~900 advisory locks for the
whole evidence write, which defeats the batch-size rationale and is serialization
rather than a fix.

## Invariant after the change

A memo row never exists unless the partition's evidence AND its readiness are
already durable: memo and phase rows commit atomically together, strictly after
every evidence batch for that partition has committed.

The reverse direction is tolerated and self-healing. Committed evidence with no
memo row is what a failed fan-in, a skipped partition, and a process that died
between the last batch commit and the fan-in all leave behind — identically,
because a transaction that does not commit writes nothing. A partition with no
memo row is a gate MISS in `applyDeferredPartitionMemoGate` and always reloads;
the next pass re-discovers the same evidence, re-upserts it as a no-op
(`relationship_evidence_facts` is content-addressed with
`ON CONFLICT (evidence_id) DO NOTHING`), and publishes then. The phase row is an
idempotent `ON CONFLICT ... DO UPDATE` keyed by generation, so republishing
rewrites rather than duplicates.

## Deadlock freedom

The batch phase's argument is unchanged: disjoint, contiguous, sorted repository
slices, one pooled connection per batch, no nested acquisition.

The fan-in has its own. Fan-in transactions are per-partition and partitions are
disjoint repository sets, since a repository has one active generation and each
batch records it under the single partition it observed under that batch's lock.
Each fan-in transaction acquires its locks through
`acquireDeferredMaintenanceRepoExclusiveLocks`, which sorts the keys, so every
caller in the system takes them in the same global order. The fan-in runs only
after `wg.Wait()`, so it never overlaps a batch transaction of the same pass.
Sorted acquisition of a consistent global order cannot deadlock against a
concurrent ingestion commit taking the same keys, or against another pass.

## Evidence

All runs are against a throwaway `postgres:18-alpine` container on a
non-conflicting port, so they never touch the live gate's fixed ports.

### Failing test first

`TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails`
(`go/internal/storage/postgres/ingestion_backfill_fanin_publication_test.go`)
seeds two repositories under one `(scope, generation)`, forces one repository
per batch, and injects a failure on the SECOND `Begin`, so the first batch
commits and its sibling never opens. It then asserts the partition has no memo
row, no readiness row, and that `applyDeferredPartitionMemoGate` puts it in
`ToLoad`.

On unmodified `e01f01c96`:

```
--- FAIL: TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails (0.37s)
    ingestion_backfill_fanin_publication_test.go:228: deferred_backfill_partition_memo
    rows for the half-committed partition = 1, want 0; a memo here permanently
    suppresses the partition's fact load
TEST_EXIT=1
```

The falsifier — no memo row after the injected failure, or the gate returning
`ToLoad` anyway — did not trigger. The memo row was there, so the hazard is
reproduced rather than argued from structure.

After the fan-in change the same test passes, with the rest of the matrix:

```
--- PASS: TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails (0.67s)
--- PASS: TestDeferredBackfillWithholdsPublicationWhenSiblingBatchCanceled (0.52s)
--- PASS: TestDeferredBackfillPublishesOncePerPartitionAcrossBatches (0.23s)
--- PASS: TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced (0.35s)
--- PASS: TestDeferredBackfillFanInFailureLeavesEvidenceRecoverable (0.06s)
--- PASS: TestDeferredBackfillCrashBetweenBatchesAndFanInConverges (0.06s)
--- PASS: TestFanInActiveGenerationMatchesCorpusLoader (0.05s)
--- PASS: TestDeferredBackfillPublishesOneRowPerPartitionHermetic (0.03s)
--- PASS: TestDeferredBackfillWithholdsPublicationForSupersededGenerationHermetic (0.01s)
TEST_EXIT=0
```

The crash window is proven, not asserted.
`TestDeferredBackfillCrashBetweenBatchesAndFanInConverges` runs the real
evidence-batch phase and stops before publication — the exact work a process
that died after its last batch commit would have finished — then asserts the
evidence is present, no memo or readiness row exists, the gate reloads the
partition, and a clean rerun converges to exactly one readiness row, one memo
row, and ZERO new evidence rows.

Guard teeth were checked by mutation rather than assumed: replacing the fan-in's
`activeGeneration != partition.GenerationID` check with `if false` makes
`TestDeferredBackfillFanInSkipsPartitionWhoseGenerationAdvanced` fail with
`graph_projection_phase_state rows for the superseded generation = 1, want 0`.

All nine live proofs are enrolled in the reducer contention gate's `-run`
filter, and `TestReducerContentionPostgresProofsRunInTheReducerContentionGate`
fails if a rename drops one of them out of it.

No-Regression Evidence: full-package runs, same container, same env, before
and after the change:

- Hermetic (no DSN): `go test ./internal/storage/postgres -count=1` — exit 0.
- DSN-gated: the same command with `ESHU_DEFERRED_PARTITION_PROOF_DSN` set — 15
  failures, an identical set to unmodified `e01f01c96` run against the same
  container with the same env; the diff of the two failure sets is empty in both
  directions. Those 15 are pre-existing schema drift in DSN-gated test helpers
  (`column "container_image_identity_v2_required" does not exist`, `relation
  "fact_records" does not exist`) plus two Flux cross-repo proofs failing on
  `invalid byte sequence for encoding "UTF8": 0x00` in the evidence insert. None
  are on the changed path. They surface only when that DSN is set, which no
  workflow currently does.
- Race: the concurrency and fan-in proofs under `-race` — exit 0, no data race.
- `golangci-lint run ./internal/storage/postgres/...` — 0 issues.

Query-shape proof for the one new query, `activeScopeGenerationQuery`. The
fan-in needs a per-scope active-generation lookup because it runs once per
partition, and `loadActiveRepositoryGenerations` is a corpus-wide scan over
every `repository` fact — at ~910 partitions that would be ~910 corpus scans per
pass. `EXPLAIN (ANALYZE, BUFFERS)` against 910 scopes / 10,920 generation rows:

```
Limit  (cost=0.56..5.58 rows=1 width=50) (actual time=0.024..0.024 rows=1.00 loops=1)
  Buffers: shared hit=7
  ->  Nested Loop Left Join  (actual time=0.023..0.023 rows=1.00 loops=1)
        ->  Index Only Scan using scope_generations_scope_latest_lookup_idx
              on scope_generations generation  (actual time=0.010..0.010 rows=1.00 loops=1)
              Index Cond: (scope_id = 'git:scope-457'::text)
        ->  Index Scan using ingestion_scopes_pkey on ingestion_scopes scope
Execution Time: 0.034 ms
```

It uses the intended `scope_generations_scope_latest_lookup_idx`
`(scope_id, ingested_at DESC, generation_id DESC)` from migration 002, with no
sort and 7 buffer hits. `TestFanInActiveGenerationMatchesCorpusLoader` pins it
against the corpus-wide loader on the shapes where the two could diverge: a
scope whose latest generation is not its only one and whose
`active_generation_id` pins an older one, a scope relying on the `COALESCE`
fallback to the newest generation, and a single-generation scope.

Cost the fan-in adds per pass, measured on the same fixture: the ArgoCD-bearing
probe inside each fan-in transaction takes 2.34 ms for a single-partition
candidate set. Its `fact_records` access is index-scanned to that partition, but
its `latest_generations` CTE is still evaluated corpus-wide on every call. At
~910 partitions that is roughly 2 s of aggregate probe CPU per pass, about 0.3 s
wall at the 8-worker ceiling, against a pass measured in minutes. No single
fan-in transaction comes near the 1 s threshold that would falsify the
cheap-fan-in claim. This is a real if modest increase over the previous ~29
batched probes, recorded here rather than glossed. Hoisting one batched probe
outside the fan-in transactions would remove it, at the cost of the
in-transaction snapshot consistency the carve-out has today.

Observability Evidence: readiness is no longer published per batch, so one
operator-facing log line changes and two are added:

- `deferred_backfill_batch_committed` reports `partitions=Q` where it reported
  `readiness_rows=P`. The batch no longer publishes readiness, so the old field
  had no value left to report.
- `deferred_backfill_fanin_completed partitions=N published=P skipped=S
  duration_s=D workers=W`, once per pass. `published + skipped` accounts for
  every partition the pass committed evidence for, so a fan-in that ran and
  published nothing (`published=0`) is distinguishable from a fan-in that never
  ran (line absent after a batch phase that logged normally).
- `deferred_backfill_fanin_partition_skipped=true scope_id=… generation_id=…
  active_generation_id=… reason="generation_advanced_since_batch"` names the
  partition and the generation that superseded it.

The fan-in is a new concurrent stage, so it carries its own metrics rather than
being subsumed into the pass:

- `eshu_dp_deferred_backfill_fanin_duration_seconds` — histogram over the
  publication phase. `DeferredBackfillDuration` covers the whole pass and cannot
  separate publication from the evidence phase, which is the split that now
  matters because the two scale differently: batches are fixed-size, the fan-in
  is one transaction per partition. This is also the metric that makes the
  accepted ArgoCD-probe cost visible instead of grep-only.
- `eshu_dp_deferred_backfill_fanin_published_total` and
  `eshu_dp_deferred_backfill_fanin_skipped_total` — counters, incremented per
  partition, with `skipped` labeled by a closed two-value `reason` set. Together
  they account for every partition the pass committed evidence for, so a
  shortfall reads as "the fan-in was cut short" rather than as missing readiness.

They are deliberately NOT the existing
`eshu_dp_deferred_backfill_partitions_skipped_total` /
`..._loaded_total` pair, which belongs to the memo gate on the fact-load side and
answers a different question ("did this partition's facts need reloading", not
"did its readiness publish"). Reusing that counter would have blended a
publication decision into a fact-load decision and corrupted the steady-state
skip-rate signal it exists to provide.

No new span: the fan-in records its shape as attributes on the existing
`relationship.backfill_deferred` span (`fanin_partition_count`,
`fanin_published_count`, `fanin_skipped_count`, `fanin_worker_count`), matching
how the fact-load fan-out reports its shape. Nothing comparable in this path is
separately spanned, and an orphan child span would be worse than none.

No route, worker, lease, queue domain, or runtime knob changed. The existing
`DeferredBackfillDuration`, `DeferredBackfillEvidence`,
`eshu_dp_deferred_backfill_batch_duration_seconds`, and
`eshu_dp_deferred_backfill_batches_completed_total` instruments record the same
quantities as before. `deferred_backfill_completed evidence_facts=…
readiness_rows=… duration_s=…` keeps its shape, and its `readiness_rows` now
counts the partitions the fan-in published.
`go/internal/storage/postgres/README.md` was updated in the same change for the
batch line's new format and for the fan-in step itself, and
`docs/public/observability/telemetry-coverage.md` carries the new stage row.

The metrics are proven to record, not merely to exist: the success path asserts
`published` reaches 3 and the duration histogram takes exactly one observation,
and the superseded-generation path asserts `skipped` reaches 1 carrying
`reason="generation_advanced_since_batch"`. Both were mutation-checked — making
the published counter's call disappear, and making the skipped counter record 0,
each fails its test.
