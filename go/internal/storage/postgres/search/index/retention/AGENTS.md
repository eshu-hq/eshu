# AGENTS.md — storage/postgres/search/index/retention

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../AGENTS.md` for Postgres storage conventions.
3. `docs/internal/evidence/7427-search-index-predelete.md` for the measured
   statement shape and the plan it must keep.

## Invariants

- Keep `package indexretention`. Never import the parent `postgres` package
  from non-test files: the root retention store imports this leaf.
- Every batch statement keeps `scope_id = $1 AND generation_id = $2` on both
  the page and the DELETE, and the in-statement eligibility CTE (superseded,
  not the active pointer, no claimed/running/retrying work).
  `TestBatchStatementsStayOnOneGenerationAndLeaf` pins this.
- `BatchRows` changes only with a new measurement in the evidence note. Do
  not add an environment knob.
- The advisory lock class literal in the SQL must equal `LockClass`
  (`TestLockStatementsUseLockClass`); the module-wide collision scan in
  `freshness/links/lockclass_scan_test.go` reads the literal.
- A batch never waits: try lock, lock_timeout below deadlock_timeout. Do not
  replace `pg_try_advisory_xact_lock` with the waiting form.

- The root retention store (`generation_retention_policy.go`) runs this
  leaf's batches before it opens the prune transaction, and admits only
  drained generations whose advisory lock (class 7427) it takes before the
  re-lock. Never run those batches inside the prune transaction.
- Superseded is terminal. No statement in the module may move
  `scope_generations.status` out of `superseded`
  (`TestScopeGenerationStatusNeverLeavesSuperseded`), and every reader of
  `eshu_search_index_terms` or `eshu_search_index_documents` must join
  `ingestion_scopes.active_generation_id`
  (`TestSearchIndexReadersJoinTheActiveGeneration`). Both guards live in the
  root `postgres` package and enumerate the module's source.

## Verification

From `go/`:

```bash
go test ./internal/storage/postgres/search/index/retention/ -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/search/index/retention/ -run Live -count=1 -v
go test ./internal/storage/postgres/ -run 'SearchIndex|GenerationRetention' -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
