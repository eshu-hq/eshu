# Reopen input fingerprint (#8283)

Scope: how the deferred-maintenance reopen skips `deployable_unit_correlation`
items whose inputs have not changed. Code: `backfill/input_fingerprint_store.go`
(store, DDL, upsert), `backfill/input_fingerprint_sql.go` (the one fingerprint
expression, the fingerprinted fused read, the reopen listing),
`relationship_store_resolved.go` (the fused-read method),
`ingestion_reopen_correlation.go`, migration 192.

## Why

The maintenance pass replays every succeeded `deployable_unit_correlation`
item above the replay floor on every shard drain. On the production-shaped wave
that was 799 items, each one a handler run and a Postgres write, although the
resolved relationships they read had not changed. The item can only be stale
when a relationship generation it read changed after it succeeded.

## Contract

- The handler records, per work item, the candidate repository ids its fused
  fenced read used and a fingerprint of the read. The row lives in
  `reducer_work_input_fingerprints` (`work_item_id` primary key, cascading
  foreign key to `fact_work_items`). It is an idempotent upsert, written after
  the graph writes and before the ACK. A write failure fails the attempt.
- The fingerprint is taken inside the fused fenced read
  (`GetResolvedRelationshipsForReposWithCorpusFenceAndInputFingerprint`), in the
  same statement snapshot as the corpus fence and the rows. A second statement
  would pair a later generation state with earlier rows.
- The value is defined once, by `backfill.InputFingerprintExprSQL`: the item's
  own relationship generation (id, status, `activated_at`, resolved-row count)
  plus every active generation with a resolved row touching the stored repo ids
  (id, `activated_at`, touching-row count). It is a SHA-256 computed in SQL on
  both sides. The production database is FIPS-mode, where `md5()` fails.
- The reopen listing (`ListSucceededDeployableUnitWorkItemsQuery`) recomputes
  the value from the stored `repo_ids` and the work item's generation and
  returns an `unchanged` flag per item. It is a flag, not a filter. A missing
  row is `unchanged = false`, so the item reopens. The other three correlation
  domains keep the blanket listing.
- The listing runs in the pass's one reopen transaction, after the
  deployment_mapping and code_import_repo_edge reopens. A listing error rolls
  the whole transaction back, which loses that pass's other reopens until the
  next pass.

## What it does not do

- It is a generation fingerprint, not a content hash. A re-activation that
  changes no content still changes `activated_at`, so such an item reopens (an
  unneeded reopen is allowed, a skipped one is not). A content hash is a
  measured follow-up.
- Items with no workload candidates never record a fingerprint and keep
  reopening every pass.
- The partition-scoped targeted pass keeps its blanket correlation reopen.
- It does not look at readiness gates. A readiness filter on the listing is
  unsafe: the fence and canonical gates are corpus-wide and the same
  transaction reopens deployment_mapping first.

## Invariants and proof

Resolved rows are inserted with `ON CONFLICT DO NOTHING`, one statement per
row, and the generation is activated in a later transaction
(`reducer/crossrepo/cross_repo_resolution.go`, steps 4 and 6). Rows can
therefore be added to an active generation before `activated_at` moves. The
row counts in the fingerprint cover that window. They are only sound while
`resolved_relationships` stays append-only, which
`TestResolvedRelationshipsStayAppendOnly` enforces over every Go source and
migration.

Proofs: `input_fingerprint_reopen_live_test.go`,
`input_fingerprint_interleave_live_test.go`, `input_fingerprint_sql_test.go`,
`ingestion_reopen_correlation_unchanged_test.go`, and
`docs/internal/evidence/8283-reopen-input-fingerprint.md`.

## Rollout order

Apply migration 192 with the schema Job before the reducer image rolls, not
only before the ingester. An ingester binary that rolls first fails the listing
on the missing table and rolls back that pass's whole reopen transaction. A
reducer binary that rolls first fails `recordInputFingerprint` after the graph
writes; that error has no class in `nonCountingReducerRetryFailureClasses`, so
it takes the counting retry path and an item with candidates can dead-letter,
and the succeeded-only reopen never brings it back.

## Accepted risk

The blanket reopen also repaired graph-only losses; a skipped item is not
rewritten. A stalled worker whose lease expired can write a stale graph between
the current worker's graph write and its fingerprint record, and the current
fingerprint then keeps the item skipped on that stale graph. Accepted, not a
code change.
