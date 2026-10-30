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

The `targeted` child owns partition-scoped maintenance and its reopen
transaction. Three shipped whole-pass reopen listings and shared replay-floor
fragments live in `reopen_sql.go`; root keeps constant aliases for existing
consumers. The child derives bounded listings once from those strings. The
#8294 completion wake in the parent package is the one other composer: it
imports the exported `ScopeReplayFloorCTE` / `ScopeReplayFloorJoins`
aliases so its candidate set carries this same single floor text.

`reopen_fingerprint_sql.go` holds the `deployable_unit_correlation`
changed-evidence gate (#8283): the input-row predicate and digest both the
fused read and the reopen listing render, the gated listing
`ListDeployableUnitReopenCandidatesQuery` (the same floor fragments plus an
`input_changed` column, the stage marker exactly once), its scanner, and the
claim-fenced success write. resolved rows are immutable, so the sha256 of the
sorted `resolved_id` set identifies their content. The listing takes no lock.
The same file holds the #8316 graph-loss repair SQL: the fingerprint
invalidation for removed repositories (single `$1 = ANY(repo_ids)` and batch
overlap forms) and the bounded admitted-anchors listing feeding the gap
reconciliation, plus the anchors scanner.

## Exported surface

- `Store`, `NewStore` — engine handles plus the two seams the engine must
  not import from the parent: the catalog loader and the evidence/phase
  store constructors
- `RunAllRelationshipEvidence`, `RunDeferredBackfillBatchesWith`,
  `PublishDeferredBackfillPartitions`,
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
- `RefuseDestructiveSuccessor` — the #7926 successor-emptiness backstop
  predicate: refuses an empty or sharply shrunken successor over a
  content-bearing prior unless the generation carries trusted deletion.
  The shrink arm compares like for like (#8750): the root passes the
  prior's collector rows (the reducer-prefixed rows a reducer writes back
  into a generation are excluded), never every row of the generation.
- `DecideSuccessor`, `SuccessorGuardInput`, `SuccessorGuardDecision`,
  `DecideDeltaFileUnion` — the #8750 decision for one commit. A full
  successor goes through `RefuseDestructiveSuccessor` against the prior's
  collector rows. A git delta successor is sized by its projected file
  union against its anchor, the newest projected full generation at
  `DeltaBaselineCommitSHA`, resolved by the root with the same resolver the
  projector overlays on. `DecideSuccessor` first applies the zero-query
  lower bound `anchorFiles - |touched ∪ live| + live`. A bound at or above
  half the anchor's files stores. Under half it sets `NeedsProbe`, the
  root counts the anchor files inside `touched ∪ live` with one probe, and
  `DecideDeltaFileUnion` refuses only when the exact union holds under half
  the anchor's files. A delta with no anchor stores, so the projector's
  base-missing refusal arms the reindex watermark. The `Rule` it returns is
  one of the closed `telemetry.SuccessorGuardRule*` values. Its
  `PriorCollector` and `PriorOrphan` come from the larger of the
  duplicate-check candidate and the scope's active generation (#8750 F1),
  which the root picks before calling it, and re-picks on the commit
  transaction when the scope changed after that sizing (#8750 window).
  Covered collectors (each with an explicit operator attestation surface,
  #8267, #8329): `jira` (`allow_empty_successor` target key),
  `pagerduty` (`allow_empty_successor` target key), `confluence`
  (`ESHU_CONFLUENCE_ALLOW_EMPTY_SUCCESSOR`), `aws` (`allow_empty_successor`
  scope key or `ESHU_AWS_ALLOW_EMPTY_SUCCESSOR`), `oci`
  (`allow_empty_successor` target key or
  `ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR`), `tfstate`
  (`allow_empty_successor` runtime key or `ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR`,
  gated on the legitimate-empty shape: snapshot only, zero body facts), and
  component extensions (SDK `Generation.trusted_deletion`, forwarded by the
  extension host).
  Uncovered collectors (for example `security_alerts/alertruntime`) have no
  attestation path: a legitimately emptied scope stays on its prior generation
  and the refusal recurs every tick under `destructive_successor_refused`
  until the scope regains content.
- `BuildDeferredScopedFactQueryParams`, `DeferredScopedFactQueryParams`,
  `BuildDeferredRepoIDRegex`, `DeferredRepoIDReferenceKeys`,
  `DeferredCatalogFingerprint`, `NewDeferredBackfillPartitionMemoStore`,
  `ApplyDeferredPartitionMemoGate`,
  `ListUnchangedPartitionsNeedingPublicationQuery` — query-arg derivation,
  memo gating, and the fan-in unchanged-partition check
- `LatestGenerationCTE`, `List*Query`, `Active*Query` constants and the
  `DeferredRelationshipFamily*SQL` fragments — the shipped SQL the root
  differential proofs derive from, never hand-copy
- `ScanFactEnvelope` — shared fact-envelope row scanner;
  `ScanFactEnvelopeWithPayloadSize` also returns the raw payload byte length
  for loaders that report scanned bytes (#9567)

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
  `deferred_backfill_fanin_partition_skipped` (per withheld partition),
  `deferred_backfill_fanin_unchanged_checked` (per whole pass with unchanged
  candidates; `deferred_backfill_fanin_unchanged_check_failed` when the
  readiness check fails and the fan-in publishes every partition)
- Spans: `relationship.backfill_deferred` — fan-out attributes land on it;
  the root seam tests pin them. Eight child spans, one per block
  (`relationship.backfill.catalog_load`, `params_build`, `partition_list`,
  `memo_gate`, `fact_load`, `discover`, `write_batches`, `fanin`), and one
  `relationship.backfill.reopen` span per reopen domain call from the root
  package (#8082). Each ends with one log line:
  `deferred_backfill_block_completed`, or `deferred_backfill_block_failed`
  when it fails the pass, or `deferred_backfill_block_degraded` when it
  errored but the pass fell back (the memo gate). `ReopenBlockAttrs` supplies
  one fresh ordered domain/count attribute slice to both whole and targeted
  reopen blocks; the targeted block appends `pass=targeted`. See
  `block_telemetry.go`; the `Block*` constants are the closed set, and
  `docs/public/reference/telemetry/deferred-backfill-blocks.md` lists the
  attributes.
- Metrics: served through the `Instruments` the root supplies on `Store`.
  `eshu_dp_deferred_backfill_block_duration_seconds{block}` records one
  sample per block per whole pass (#8082); the partition-scoped pass passes
  nil instruments and records none

## Catalog read projection (#8082)

`LoadRepositoryCatalog` asks Postgres for one object per repository fact that
holds only the keys `relationships.RepositoryCatalogPayloadKeys()` declares
(`jsonb_to_record` into typed `jsonb` columns, one detoast per row, guarded by
`jsonb_typeof(payload) = 'object'`), then decodes that object in Go. The
statement text is rendered from the key list, so it cannot name a key
`RepositoryCatalogEntry` does not read. Newest-row-wins ordering
(`observed_at DESC, fact_id DESC`) and the `observedAt` freshness map are
unchanged.

Performance Evidence: on a 28,000-row synthetic set (about 4,925 MB of
payloads, 4 percent over 1 MB, largest about 5 MB),
`BenchmarkLoadRepositoryCatalogDecode` decodes the whole payloads in 23.6 to
28.6 s per pass over three runs (21.7 GB allocated, 576 M allocations) and the
projected objects in 0.044 to 0.046 s per pass (4.97 MB, 1.1 M allocations). Server-side
projection cost on a read replica was 3.6 s with 6.8 MB shipped, against 4.4 GB
shipped before.

No-Observability-Change: the loader adds no metric, span, or log. The
existing `eshu_dp_deferred_backfill_duration_seconds` histogram and the
`relationship.backfill_deferred` span keep timing the pass that contains this
read, and the `catalog_load` block (#8138) times the whole catalog loader
call.

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
- The whole-pass fan-in uses the same revised set (#8082). An unchanged
  partition whose readiness row exists is not republished: no lock, no
  transaction, no ArgoCD probe, and the counter
  `eshu_dp_deferred_backfill_fanin_skipped_total{reason="unchanged_since_memo"}`.
  One batched `ListUnchangedPartitionsNeedingPublicationQuery` read decides.
  A missing readiness row republishes the partition; a scope that moved to a
  new generation sends it to the locked path, which declines it with
  `generation_advanced_since_batch` as before; a failed read publishes all of
  them. Memo `committed_at` then records the publication that last changed a
  partition, still in activation order (see `targetedCatalogBaselineQuery`).
  `PublishDeferredBackfillPartitions`, which the partition-scoped pass calls,
  still publishes every partition. The invariant is on
  `splitUnchangedDeferredPartitions` in `fanin_unchanged.go`. Evidence is in
  `docs/internal/evidence/8082-fanin-unchanged-partitions.md`.
- Differentials derive from the shipped constants. A hand-frozen copy
  drifts and goes false-green.
- `DecideSuccessor` does no I/O and never refuses on its lower bound alone:
  the bound subtracts every touched or live path as if it were an anchor
  file, so it can only under-estimate the union. A failed bound means
  "probe", and only `DecideDeltaFileUnion` over the exact count can refuse a
  delta (#8750). The root owns the counts, the probe, the
  `successor_guard` log stage, and
  `eshu_dp_ingestion_successor_guard_decisions_total`; this package owns
  only the verdict and the closed rule names.

## Related docs

- `docs/public/architecture.md` (pipeline: sync to query surface)
- `docs/internal/evidence/8750-successor-guard-sizing.md` (successor guard
  sizing)
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
