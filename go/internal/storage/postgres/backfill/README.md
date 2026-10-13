// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

# Deferred Backfill Engine

## Purpose

The backfill package owns the deferred relationship backfill engine:
corpus-wide backward-evidence discovery, bounded per-repository batch
writes, the per-partition fan-in that publishes readiness and the
partition memo, the per-commit new-repository backfill, and the
generation freshness guard the commit path consults. It was extracted
from the postgres package root (issue #7794) so the engine logic ships
separately from commit-path orchestration.

## Ownership boundary

The engine owns pure backfill logic, its SQL, and the `Store` seam. The
root `IngestionStore` owns commit-path orchestration: building a `Store`
with all seams supplied (`backfillStore`), the post-commit transaction,
the skip orchestration, the reproject transaction, and the catalog cache.
The engine never imports the parent postgres package.

## Exported surface

- `Store`, `NewStore` — engine handles plus the two seams the engine must
  not import from the parent: the catalog loader and the evidence/phase
  store constructors
- `RunAllRelationshipEvidence`, `RunDeferredBackfillBatchesWith`,
  `BackfillRelationshipEvidenceForNewRepositories`, `BackfillScopedCatalog`
  — pass and per-commit entry points
- `LoadRepositoryCatalog`, `LoadActiveRepositoryGenerations`,
  `LoadActiveGenerationForScope`, `LoadActiveScopeGenerationPartitions`,
  `LoadDeferredAnchorScopedRelationshipFacts`,
  `LoadDeferredScopedFactsAcrossPartitions` — loaders the root seam and
  the proofs drive
- `DecideUnchangedGeneration`, `UnchangedGenerationCandidate`,
  `GenerationNeedsDuplicateCheck`, `RecordSkippedRefresh` — the duplicate
  decision and its skip accounting
- `BuildDeferredScopedFactQueryParams`, `DeferredScopedFactQueryParams`,
  `BuildDeferredRepoIDRegex`, `DeferredRepoIDReferenceKeys`,
  `DeferredCatalogFingerprint`, `NewDeferredBackfillPartitionMemoStore`,
  `ApplyDeferredPartitionMemoGate` — query-arg derivation and memo gating
- `LatestGenerationCTE`, `List*Query`, `Active*Query` constants and the
  `DeferredRelationshipFamily*SQL` fragments — the shipped SQL the root
  differential proofs derive from, never hand-copy
- `ScanFactEnvelope` — shared fact-envelope row scanner

See `doc.go` for the full contract.

## Dependencies

- `internal/storage/postgres/db` — `ExecQueryer`, `Transaction` contracts
- `internal/relationships` — evidence discovery and merge
- `internal/scope` — scope and generation types
- `internal/telemetry` — instruments carried on `Store`
- `internal/facts` — envelopes

## Telemetry

- Logs: `deferred_backfill_batch_committed` (per batch),
  `deferred_backfill_fanin_completed` (per pass),
  `deferred_backfill_fanin_partition_skipped` (per withheld partition)
- Spans: `relationship.backfill_deferred` — fan-out attributes land on it;
  the root seam tests pin them
- Metrics: served through the `Instruments` the root supplies on `Store`

## Gotchas / invariants

- Batches publish nothing partition-wide. Readiness and memo rows come
  only from the per-partition fan-in after every batch commits; a batch
  holds only some of a partition's repositories.
- Evidence inserts stay content-addressed (`ON CONFLICT DO NOTHING`) so a
  re-run converges.
- `Store` with nil `NewEvidenceStore` or `NewPhaseStore` fails the write
  path fast. Unit tests wire recording doubles via `testStore`; durable
  assertions belong to the root live seam tests.
- The memo-hit skip set is revised from actually-inserted row counts, so a
  partition that receives new rows reopens even when the loader skipped it.
- Differentials derive from the shipped constants. A hand-frozen copy
  drifts and goes false-green.

## Related docs

- `docs/public/architecture.md` (pipeline: sync to query surface)
- `docs/public/reference/cypher-performance.md` for hot-path query work

## No-Regression Evidence (#7794 extraction):

Baseline: origin/main with the engine at the postgres root. After: this
package move at the PR head (exact head bound by the CI wave recorded in
the PR). Backend: hermetic (no database); DSN-gated live proofs run in the
CI postgres_ci lane. Input shape: package move plus seam, zero query-text
deltas — every moved SQL constant resolves byte-identically to main's text
(verified with a const resolver; the only textual diffs are Go identifier
renames that resolve to identical values). Batch sizing, worker counts,
lock order, transaction scope, and upsert conflict targets are unchanged.
Terminal counts: `go test ./internal/storage/postgres/...` passes with
zero FAIL lines across root, backfill, and all subpackages;
`golangci-lint run` reports 0 issues.

No-Observability-Change: metric names in `go/internal/telemetry/
instruments.go` are untouched, and the engine emits the same log lines
(`deferred_backfill_batch_committed`, `deferred_backfill_fanin_completed`,
`deferred_backfill_fanin_partition_skipped`) and the same
`relationship.backfill_deferred` span attributes through the `Instruments`
the root seam supplies. The telemetry-coverage doc rows now point at the
new file locations with unchanged metric columns.
