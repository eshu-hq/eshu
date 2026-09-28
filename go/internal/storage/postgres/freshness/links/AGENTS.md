# AGENTS.md — changed-since link ledger store

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `link_sql.go` and `journal_sql.go` for every statement, and `link.go` for
   the lock order.
4. `docs/internal/evidence/7127-changed-since-link-writer.md` for the gates.

## Invariants

- The lock order in `LinkWriter.linkInTx` is: cursor row, then generation row,
  then slot. Every step is non-blocking (`SKIP LOCKED` or
  `pg_try_advisory_xact_lock`). A miss returns `*RetryError` and never
  success, because returning success would drop the activation.
- Compute every digest in SQL from `PayloadDigestInput`. Never hash a payload
  in Go, because Go cannot reproduce `jsonb::text`. Changing the digest input
  or the state construction bumps `DigestVersion`, which re-roots every scope.
- A chain break keeps the state and advances only `state_activation_seq`
  (#7127 ruling 8.5).
- Lock misses never count an attempt and write nothing. Only a failure once
  the link statement ran is a counting `*FailureError`, and only
  `RecordFailure`, under the cursor lock and after the rollback, writes the
  attempt columns. At the limit it poisons (a `link_poisoned` break), never
  halts (#7127 ruling 8.10). Never write attempt state to
  `changed_since_activations`: PR-3b's Ack lock-order proof depends on it.
- The incremental statement's `del` deletes by the `ctid` array the diff read
  and must carry no indexable predicate on the target: no key join and no
  `scope_id = $1`. Either gives the planner an index path that goes
  quadratic, or scans the whole scope, when the scope's state rows are
  estimated at one (the G8 stall, arbiter ruling arb-7127-g8). The scope
  guard is `RETURNING t.scope_id` plus the row-count invariant in
  `incremental()`; never drop the invariant, because without it a broken
  fence silently skips a delete. `TestIncrementalLinkPlanClassUnderPlantedStatistics`
  is the gate.
- No foreign key may be added to any ledger table (gate G13). See
  `README.md` for the reason.
- The overlay link is not shipped. A delta activation is a break until the
  delta-kind ownership proof (G2) passes in its own PR.
- The ledger delete of retention is one statement (`retentionPruneQuery`):
  links, deltas, bucket counts and activations in one `WITH`, one snapshot.
  Never split it; split deletes leak headless deltas
  (`TestSplitLedgerDeleteLeaksHeadlessDeltas`, arbiter ruling arb-7127-3d).
  `RowsPruned` comes from its returned counts, not from the pre-count.
- The retention statements read `changed_since_links` once, with array
  filters and no join or per-scope LATERAL: stale statistics plan either as
  one sequential scan per scope. Deltas and bucket counts go through a
  LATERAL `OFFSET 0` probe of the full key prefix, then a ctid delete.
  `TestRetentionStatementPlanShape` is the gate; keep its planted RED.
- Retention never touches `changed_since_key_state` or
  `changed_since_scope_cursor`, deletes an activation only for its own
  `generation_id` (never for `prior_generation_id`), and does not depend on
  the link switch (#7127 ruling 2.8).
- `BatchRowLimit` counts ledger rows. A candidate over it only because of
  its ledger rows is pruned in a batch of its own (arbiter ruling
  arb-7127-3d-b); never extend that admit-alone rule to rows outside the
  ledger, whose cascades are unmeasured at that size. The limit re-check
  after a skip is capped at `generationRetentionRecheckLimit` rounds. A batch
  of one over the limit is narrowed to its own scope and generation row
  (savepoint rollback, targeted re-lock, recount); `SET LOCAL work_mem` stays
  before the savepoint, and a batch within the limit is never narrowed.
- The bound: at most one link per scope whose prior was pruned (plus its
  deltas and bucket counts), removed with its own generation, and at most one
  backfill chain of activation rows per racing backfill episode, removed with
  the scope. See `README.md`, "Retention". An activation ends as one link, one
  break, or deleted by retention before it is linked.
- Do not import the parent `postgres` package from non-test code. The parent
  imports this package for `PayloadDigestInput`.

## Verification

```bash
cd go && ESHU_POSTGRES_TEST_DSN=postgres://... go test ./internal/storage/postgres/freshness/links -count=1
```
