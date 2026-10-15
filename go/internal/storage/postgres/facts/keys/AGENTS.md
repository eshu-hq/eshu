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

## Common changes

- A new per-fact key sidecar table joins `Deletes()`, gets a
  `(scope_id, generation_id, fact_id)` index migration, and is added to the
  kind list in the parent's `fact_delete_key_guard_test.go`.
- A new fact delete site calls `DeleteGenerations` or `DeletePage` first and
  is classified in `factDeleteSites`.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/facts/keys -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres -run 'FactKey|FactRecordDelete' -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
