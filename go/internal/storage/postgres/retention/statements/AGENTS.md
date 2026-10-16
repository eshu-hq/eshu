# AGENTS.md — storage/postgres/retention/statements

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `../../generation_retention_sql.go` for the general candidate query that
   `TargetedCandidateLock` must stay in lockstep with.

## Invariants

- SQL text only. No functions, no imports, no state. The parent
  `postgres` package binds and runs the statements.
- Never import the parent `postgres` package; the parent imports this leaf.
- Keep `TargetedCandidateLock` in lockstep with the parent's
  `generationRetentionCandidateQuery`: the same eligibility rules, the
  `FOR UPDATE OF generation, scope SKIP LOCKED` lock, deterministic
  `(scope_id, generation_id)` order, and the same output columns.
- This is hot-path Postgres SQL. A change to its plan needs an
  `EXPLAIN (ANALYZE, BUFFERS)` measurement before it lands
  (`eshu-postgres-rigor`).

## Verification

From `go/`:

```bash
go test ./internal/storage/postgres -run 'Retention|Targeted|DeltaBase|SameSHA|HardCeiling' -count=1
```

The re-lock live proofs in the parent package
(`generation_retention_relock_live_test.go`) read this text. Run them with
the retention family DSN. Run `scripts/verify-package-docs.sh` from the
repository root.
