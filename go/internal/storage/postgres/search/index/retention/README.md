# Search index retention pre-delete

## Purpose

Deletes a superseded generation's `eshu_search_index_terms` and
`eshu_search_index_documents` rows in small committed batches before
generation retention prunes the generation (#7427 PR1). The retention prune
then deletes the `scope_generations` row with the search index already empty,
so the foreign-key cascade it runs under the scope row lock has nothing to
remove for those tables.

## Ownership boundary

This leaf owns the batch loop, its SQL, and the per-generation advisory lock
class. It does not choose candidates, decide eligibility policy, or prune
generations: `storage/postgres` (the retention store) reads the candidates,
calls `Pass.Run` before it opens its prune transaction, admits only drained
generations, and calls `TryLockGenerations` inside that transaction. It never
writes search index rows; the reducer projection writer
(`internal/reducer/eshusearch`) does.

## Flow

```mermaid
sequenceDiagram
    participant Store as retention store
    participant Pass as indexretention.Pass
    participant PG as Postgres
    Store->>PG: candidate read (FOR NO KEY UPDATE SKIP LOCKED), ROLLBACK
    loop each candidate, oldest first, within the budget
        loop terms, then documents
            Pass->>PG: BEGIN; try advisory lock; keyset batch DELETE; COMMIT
        end
        Pass->>PG: from-start re-sweep until a page is empty
    end
    Store->>PG: BEGIN prune; select; try advisory locks; re-lock; prune; COMMIT
```

## Exported surface

- `Drain(ctx, db.Beginner, Generation, *Budget) (Report, error)`
- `Pass`, `NewPass`, `(*Pass).Run`, `Drained`, `Report`, `Totals`, `Ran`
- `Budget` (`RowCap`, `Deadline`, `Exhausted`, `Batches`, `Rows`)
- `Generation`, `Report`, `Outcome` and its closed constants
- `TryLockGenerations(ctx, db.Queryer, ids)` for the prune transaction
- `BatchRows` (20,000) and `LockClass` (7427)

See `doc.go` for the contract.

## Dependencies

- `internal/storage/postgres/db` for `Beginner`, `Queryer`, `Rows`.
- `internal/telemetry` for the log keys and phase attribute.
- `github.com/jackc/pgx/v5/pgconn` to classify lock-timeout and deadlock
  errors.
- Tests only: `internal/storage/postgres` (`ApplyBootstrap`) and
  `internal/testutil/postgresproof` for the live proofs.

## Telemetry

- Logs: `generation retention search index pre-delete completed` per
  generation (scope_id, generation_id, outcome, drained, batches, rows per
  table, duration, sweep seconds); info when rows were deleted or the
  generation did not drain, debug for an empty drained generation.
- Metrics are emitted by the retention runner from the store's result:
  phases `predelete_search_index` and `sweep_search_index` on
  `eshu_dp_generation_retention_phase_duration_seconds`, rows on
  `eshu_dp_generation_retention_rows_pruned_total{table}`, skip reasons
  `search_index_undrained` and `advisory_lock_busy` on
  `eshu_dp_generation_retention_skipped_total`, and
  `eshu_dp_generation_retention_search_index_batches`.

## Gotchas / invariants

- Every batch statement filters on `scope_id = $1` and `generation_id = $2`
  in both the page and the DELETE. The ctid match is valid only because
  `scope_id` prunes both to one hash leaf; never drop either predicate.
- The terms walk measured Index Scan on the leaf primary key, Index
  Searches 1, and runtime pruning (Subplans Removed: 63) under a generic plan;
  the first and a late batch cost the same. See
  `docs/internal/evidence/7427-search-index-predelete.md`.
- A generation is drained only when a from-start page returns no row. A short
  page is not enough: the re-sweep catches a row inserted behind the cursor.
- Never run a batch inside the retention prune transaction or under its scope
  locks.
- In this change the foreign keys stay, so the locked cascade still scans the
  terms leaf (cost follows the leaf, not the generation) until the terms
  foreign key is dropped in a later change.
