# AGENTS.md — changed-since link ledger store

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `link_sql.go` and `journal_sql.go` for every statement, and `link.go` for
   the lock order.
4. `docs/internal/evidence/7127-changed-since-link-writer.md` for the gates.

## Invariants

- The lock order in `LinkWriter.linkInTx` is: cursor row, then the activating
  generation, then the prior generation, then slot. The cursor lock
  (`FOR UPDATE SKIP LOCKED`) and the slot (`pg_try_advisory_xact_lock`) never
  wait. A miss returns `*RetryError` and never success, because returning
  success would drop the activation.
- A generation lock (`FOR KEY SHARE SKIP LOCKED`: the activating generation,
  the prior, and the backfill's insert) can wait. PostgreSQL walks the row's
  update chain before it applies the wait policy, and the walk waits without
  condition on a later version that another transaction holds `FOR UPDATE` or
  is deleting (`heap_lock_updated_tuple_rec`; `NOWAIT` waits there too). So
  every generation lock runs under a transaction-local `lock_timeout` of
  `generationLockTimeout` (250 ms), and SQLSTATE 55P03 from it is the
  non-counting `generation_lock_timeout`. Keep the timeout below the server's
  `deadlock_timeout`. Reset it to 0 before the link statement, which runs
  under `statement_timeout` alone. Map 55P03 only at the generation locks,
  never in `ClassifyFailure`. Do not change the lock to `FOR SHARE`: it does
  not wait, but it blocks Ack and heartbeat for the length of a link. The
  gates are `TestActivatingGenerationChainWaitTimesOut`,
  `TestPriorChainWaitTimesOut` and `TestGenerationLockChainWaitRaceShape`;
  keep their planted RED (arbiter ruling arb-7127-3e-wait).
- Every ledger writer holds `FOR KEY SHARE` on every generation a row it
  writes names, until it commits (arbiter ruling arb-7127-3d, C4). Only then
  is retention's ledger delete complete. A new writer (PR-3c's `pairwise`
  link first) locks both generations it names, `SKIP LOCKED` under the
  generation lock timeout, after the cursor and before the slot, and never names a generation it did not lock.
  An absent prior means a rebase (`RebaseLinkSQL`: root link, no deltas, no
  buckets, `prior_pruned`), never a link naming the pruned generation, and
  never a re-root of the whole scope.
- `RebaseLinkSQL` and `IncrementalLinkSQL` share `stateDiffCTE` and
  `stateMoveCTEs`. Change them together, and keep the rebase's row-count
  invariant.
- The backfill inserts each activation through its generation row
  (`insertActivationQuery`, `FOR KEY SHARE OF generation SKIP LOCKED`); a row
  not inserted ends the scope's chain for the pass.
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
- With the writer rule, no link or activation names a pruned generation;
  `eshu_dp_changed_since_ledger_orphans` (the orphan probe) watches it. See
  `README.md`, "Retention". An activation ends as one link (a rebase is a
  link with the `prior_pruned` break), one break, or deleted by retention
  before it is linked.
- Do not import the parent `postgres` package from non-test code. The parent
  imports this package for `PayloadDigestInput`.

## Verification

```bash
cd go && ESHU_POSTGRES_TEST_DSN=postgres://... go test ./internal/storage/postgres/freshness/links -count=1
```
