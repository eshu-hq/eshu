# AGENTS.md — fact key sidecar delete guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions ("Key sidecar deletes
   (#7420)").
3. `docs/internal/evidence/7420-key-sidecar-explicit-deletes.md` for the
   measured plans.

## Invariants

- Keep the `Deletes()` order: relationship, manifest, registry. Changing it
  can deadlock against the package key scope rebuild
  (`TestRetentionFactKeyReversedOrderDeadlocksLive` in the parent package).
- Keep `scope_id` and `generation_id` as the leading predicates of every
  statement, so each plan stays on the partition index. Do not add a
  `fact_id`-only form: it plans as a sequential scan
  (`TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive`).
- Run in the caller's transaction, before the fact delete. Never commit
  inside this package.
- Keep the package a leaf: never import the parent `postgres` package.
- No foreign key backs these tables any more (migrations 188 to 190). Keep
  `OrphanCensusSQL` joining on `(fact_id, scope_id, generation_id)`, the
  triple the readers join on
  (`TestFactKeyOrphanCensusCountsPlantedOrphansLive` plants a key whose
  fact lives in another generation).
- A test that truncates or deletes `fact_records` clears the key sidecars
  too. A new `TRUNCATE` of `fact_records` in the parent package is
  classified in `factTruncateSites` (`fact_truncate_key_guard_test.go`) with
  the key tables it names.
- A migration that drops a foreign key to `fact_records` locks
  `fact_records` first, then the child with `NOWAIT` (migrations 188 to
  190). A plain `DROP CONSTRAINT` deadlocks ingestion's fact-then-key order.

## Common changes

- A new per-fact key sidecar table joins `Deletes()`, gets a
  `(scope_id, generation_id, fact_id)` index migration, and is added to the
  kind list in the parent's `fact_delete_key_guard_test.go`. Its partition
  index joins `UnavailablePartitionIndexesSQL`, so retention refuses rather
  than scan when the index is missing or invalid.
- A new statement in the parent package that deletes `fact_records`
  (directly, or through a `scope_generations` or `ingestion_scopes` cascade)
  calls `DeleteGenerations` or `DeletePage` first in the same transaction,
  or is scoped to fact kinds that own no rows in the three key tables. Either
  way it is classified in `factDeleteSites` (`fact_delete_key_guard_test.go`).

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/facts/keys -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres -run 'FactKey|FactRecordDelete|FactRecordsForeignKey' -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
