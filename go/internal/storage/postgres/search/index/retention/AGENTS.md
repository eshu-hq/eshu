# AGENTS.md — storage/postgres/search/index/retention

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../AGENTS.md` for Postgres storage conventions.
3. `docs/internal/evidence/7427-search-index-predelete.md` for the measured
   statement shape and the plan it must keep, and
   `docs/internal/evidence/7427-search-index-orphan-guard.md` for the orphan
   census plan and the c3 freshness change.

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
- The orphan sweep's eligibility CTE keeps both tests: the pair is absent
  from `scope_generations`, and `ingestion_scopes.active_generation_id` does
  not name it (`TestOrphanSweepStatementsDeleteOnlyMissingGenerations`,
  `TestOrphanSweepLiveNeverDeletesAnExistingGeneration`). It shares the
  pre-delete's page and DELETE body; change both together.
- The census stays a loose index scan per terms leaf, never `DISTINCT` over
  a table (`TestCensusStatementWalksThePrimaryKeyPrefix`).
- The #8231 superseded sweep (`sweep.go`) lists still-existing superseded
  generations oldest first (cap `SupersededSweepCandidateLimit`) and drains
  each with the fail-closed batches plus its single stats row. Its
  eligibility CTE mirrors `eligibleGenerationCTE` and additionally requires
  `scope.active_generation_id IS NOT NULL`; keep the shared page and DELETE
  bodies identical (`TestSupersededSweepStatementsFailClosed`,
  `TestSupersededSweepCandidatesListOnlySweepableGenerations`). It runs only
  through the runner's off-by-default flag; never default it on, and never
  enable it until the issue's gate-(b) dead-tuple/bloat evidence lands.
- Never run the census or the sweep inside a retention transaction.
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
- The orphan sweep relies on writers of a pruned generation stopping. The
  root `NewGenerationFreshnessCheck` (`generation_freshness.go`, used by the
  reducer runtime guard and the search document, container image identity,
  cloud inventory and repo dependency handlers) returns terminal
  `(false, nil)` for a missing generation row, with or without an active
  generation (#7427 PR2). Keep it failing closed
  (`TestGenerationFreshnessFailsClosedForAPrunedGenerationLive`).
- Freshness fails closed on a missing generation (#7427 PR2):
  `NewGenerationFreshnessCheck` (root `generation_freshness.go`) returns
  `(false, nil)` when the intent's generation row is missing, also when the
  scope's active pointer is NULL. Retention prunes generation rows; since
  migration 191 dropped the terms generation foreign key nothing else rejects
  a late writer's rows. Do not restore the old `(true, nil)` for that case. An unknown scope
  stays `(true, nil)`: the search index `scope_id` foreign key rejects its
  rows. The orphan census and sweep in this leaf remove what a writer leaves
  anyway.
- Migration 191 dropped the terms generation foreign key: nothing in the
  database removes a pruned generation's terms any more. The pre-delete
  (drained-only admission) and the sweep must keep covering every
  `scope_generations` delete
  (`TestNoCodePathReliesOnTheTermsGenerationForeignKey` in the parent
  package). Never reintroduce a terms statement under the retention lock.

## Verification

From `go/`:

```bash
go test ./internal/storage/postgres/search/index/retention/ -count=1
ESHU_POSTGRES_DSN=... go test ./internal/storage/postgres/search/index/retention/ -run Live -count=1 -v
go test ./internal/storage/postgres/ -run 'SearchIndex|GenerationRetention' -count=1
```

Run `scripts/verify-package-docs.sh` from the repository root.
