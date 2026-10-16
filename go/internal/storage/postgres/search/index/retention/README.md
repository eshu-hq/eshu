# Search index retention pre-delete

## Purpose

Deletes a superseded generation's `eshu_search_index_terms` and
`eshu_search_index_documents` rows in small committed batches before
generation retention prunes the generation (#7427 PR1). The retention prune
then deletes the `scope_generations` row with the search index already empty,
so the foreign-key cascade it runs under the scope row lock has nothing to
remove for those tables.

It also takes the orphan census and runs the orphan sweep (#7427 PR2): it
finds search index `(scope_id, generation_id)` pairs whose
`scope_generations` row is gone and deletes their rows with the same batch
loop. With the generation foreign keys present (PR2) the census reads 0;
after PR3 drops the terms generation foreign key, it removes rows a late
writer leaves behind.

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
- Orphans: `TakeCensus(ctx, db.Queryer) (Census, error)`,
  `SweepOrphan(ctx, db.Beginner, Generation, *Budget)`,
  `SweepOrphans(ctx, Database, deadline, logger) (OrphanSweep, error)`,
  `NewOrphanPass`, `Database`, `TermsTable`, `DocumentsTable`,
  `OrphanListLimit` (1,000 per relation), `OrphanSweepRowCap` (2,000,000)

See `doc.go` for the contract.

## Dependencies

- `internal/storage/postgres/db` for `Beginner`, `Queryer`, `Rows`.
- `internal/telemetry` for the log keys and phase attribute.
- `github.com/jackc/pgx/v5/pgconn` to classify lock-timeout and deadlock
  errors.
- Tests only: `internal/storage/postgres` (`ApplyBootstrap`) and
  `internal/testutil/postgresproof` for the live proofs.

## Orphan census and sweep

```mermaid
sequenceDiagram
    participant Runner as retention runner
    participant Sweep as indexretention.SweepOrphans
    participant PG as Postgres
    Runner->>Sweep: after the pass, at most once per poll interval
    loop each terms leaf, then documents
        Sweep->>PG: loose index scan over (scope_id, generation_id), anti-join scope_generations
    end
    loop each listed orphan, within the budget
        Sweep->>PG: BEGIN; try advisory lock; keyset batch DELETE if the pair is still missing; COMMIT
    end
```

- The census walks each relation's primary-key prefix with a recursive
  seek, one index descent per pair. A `SELECT DISTINCT` over the table
  read the whole 10M-row leaf (104,476 buffers, 26 s); the shipped walk read
  2,443 buffers on that leaf once vacuumed, and 365,199-486,545 buffers over
  all 64 leaves and the documents table at 31,021 pairs
  (`docs/internal/evidence/7427-search-index-orphan-guard.md`).
- Dead index entries a pre-delete left (before VACUUM) are walked once per
  census: 2,800,330 heap fetches on the first walk, then 33,141 buffers for
  the shipped census statement on that leaf once their kill bits are set.
- A sweep batch deletes only while the pair is absent from
  `scope_generations` and the scope's active pointer does not name it,
  re-checked in the batch's snapshot. A generation that exists, in any
  status, loses no row.

## Telemetry

- Logs: `generation retention search index pre-delete completed` per
  generation (scope_id, generation_id, outcome, drained, batches, rows per
  table, duration, sweep seconds); info when rows were deleted or the
  generation did not drain, debug for an empty drained generation.
- Logs: `generation retention search index orphan generation swept` per
  orphan generation, same fields.
- Metrics are emitted by the retention runner from the store's result:
  phases `predelete_search_index` and `sweep_search_index` on
  `eshu_dp_generation_retention_phase_duration_seconds`, rows on
  `eshu_dp_generation_retention_rows_pruned_total{table}`, skip reasons
  `search_index_undrained` and `advisory_lock_busy` on
  `eshu_dp_generation_retention_skipped_total`, and
  `eshu_dp_generation_retention_search_index_batches`. The orphan sweep's
  signals are `eshu_dp_search_index_orphan_generations{table}`,
  `eshu_dp_search_index_orphan_rows_swept_total{table}` and phases
  `census_search_index_orphans` and `sweep_search_index_orphans`.

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
- Accepted residual (#8276 review): the batch eligibility re-check covers
  status, `superseded_at`, the active pointer and live work, not the
  delta-base (#7802) or uncovered-writer (#7903) flags. The store filters
  both once, at the candidate read. A generation that flips to protected or
  uncovered mid-drain can lose its search index rows to batches that already
  committed, and the prune then skips it. No answer changes: every search
  index reader joins `ingestion_scopes.active_generation_id`
  (`TestSearchIndexReadersJoinTheActiveGeneration`), a superseded generation
  is never active again, and neither protection reads search index rows (see
  the evidence note). Same class as the re-lock's accepted residual: a flip
  that commits after its snapshot still reaches the prune.
- The foreign keys stay through PR2, so the locked cascade still scans the
  terms leaf (cost follows the leaf, not the generation) until PR3 drops the
  terms generation foreign key. Orphan tests drop the keys only inside their
  isolated test schema.
- The census interpolates a relation name; it comes only from
  `pg_inherits` as regclass text (PostgreSQL quotes it) or a fixed table
  name, never from input.
