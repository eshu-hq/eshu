# Postgres code-reachability store

## Purpose

This package owns the #5376/#5494 dead-code reachability domain: reducer-
materialized reachable-set rows and root-kind verdicts, keyed by active
generation, with per-repository completion watermarks so dead-code reads use
a standing lookup instead of a compatibility scan over completed shared
projection intents.

## Ownership boundary

This package owns the `code_reachability_rows`, `code_reachability_repository_watermarks`,
and `code_root_verdicts` tables, their DDL, batched upsert/replace, the
active-generation lookup, and the pending-input loader (roots, Ruby class
ancestry, and Rails route facts) that feeds `codeintel.BuildCodeRootVerdicts`.
It is distinct from `internal/reducer/codeintel`, which owns the verdict
walk and projection-runner domain logic that calls through the
`InputLoader`/`RowWriter` interfaces `CodeReachabilityStore` satisfies. The
applied DDL is in the `storage/postgres/migrations` package, and
`cmd/reducer` constructs the store. This package must not import the parent
`postgres` package from non-test code.

One live test, `code_reachability_upgrade_backfill_live_test.go`, stays in
the parent `postgres` package (#6693): it defines `testSuffix`,
`openUpgradeBackfillLiveDB`, and `registerUpgradeBackfillCleanup`, and
`testSuffix` is also used by unrelated root live tests
(`recovery_refinalize_*`, `reducer_queue_workload_replay_live_test.go`,
`repo_dependency_acceptance_gate_expiry_test.go`,
`recovery_claim_token_fence_live_test.go`). This package's own external live
test (`store_route_liveness_live_test.go`, package `reachabilitystore_test`,
with the #5494 route-liveness and #7547 loader-gate proofs) keeps its own
copies, in the helper-only `live_helpers_test.go`, of the same DSN-open/suffix/cleanup helpers rather than
depend on that root file's test-only symbols, which Go does not expose
across package boundaries.

## Exported surface

- `CodeReachabilityStore` with `NewCodeReachabilityStore(database db.ExecQueryer)`:
  `EnsureSchema`, `Upsert`, `ReplaceRepositoryRows`, `ListLatestByEntities`,
  `LoadPendingCodeReachabilityInputs`.
- `CodeReachabilitySchemaSQL()` returns the DDL.
- `CodeReachabilityVerdictSchemaEpoch` is the current verdict schema epoch;
  bump it whenever verdict semantics or the watermark's truncation
  semantics change so every projected repo re-projects exactly once on
  upgrade.
- `CompleteRunGateSQL` is the completeness predicate the loader applies to
  each active acceptance run. It must stay equal to the dead-code query's
  `run_gate` (`deadCodeIncomingBoundQuery` in `internal/query`);
  `TestDeadCodeRunGateMatchesReachabilityLoaderGate` there enforces it.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the shared `ExecQueryer`/`Rows` contracts.
- `internal/reducer/codeintel` for the row, verdict, and projection-input types.
- Its own tests additionally import the parent `internal/storage/postgres`
  package (`postgres.ApplyBootstrap`, `postgres.SQLDB`) for the live
  route-liveness proof; that is a test-only, one-directional dependency and
  does not create an import cycle in production code.

## Telemetry

None here. The reducer's `CodeReachabilityProjectionRunner` (which this
store's `InputLoader`/`RowWriter` methods feed) owns runtime observability
for the projection cycle; see `go/internal/reducer/AGENTS.md`.

No-Observability-Change: this extraction moves only the store, loader, and
helpers; no metric, span, log field, worker, queue, lease, or runtime knob
changed.

## Loader contract

`LoadPendingCodeReachabilityInputs` schedules only complete, non-delta runs
(#7547). An active acceptance run is a candidate when its generation is
full, both `code_call_materialization` and `inheritance_materialization`
reducer work items succeeded with none in another status, and no
`code_calls` or `inheritance_edges` intent of the generation for the
repository is pending. Among those, it picks runs whose watermark is
missing, older than the run's newest completed intent, or stamped under an
older verdict-schema epoch, ordered `completed_at ASC, repository_id ASC`.
An active delta generation gets no new snapshot; the next full generation
does. Until then, dead-code reads for its entities fall back to the legacy
one-hop incoming read.

The candidate statement (`loader_candidates_sql.go`) gates acceptance rows
first in a `ready` CTE, then reads one `LATERAL max(completed_at)` per run
and joins the watermark once per run. Do not fold it back into one
intent-row join with `GROUP BY`: on the QA replica that shape took 4.7 to
8.4 s per 5 s poll against 0.7 to 0.9 s for this one. See
`docs/internal/evidence/7547-reachability-loader-gate.md`.

## Gotchas / invariants

- `ReplaceRepositoryRows` always stamps the watermark, even for an empty
  (no-Ruby or zero-verdict) replacement, so the upgrade-backfill predicate in
  `LoadPendingCodeReachabilityInputs` cannot loop on it forever.
- Never edit the migration files under `../../migrations`; their checksums
  are load-bearing. Change `CodeReachabilitySchemaSQL()` only alongside a new
  migration, keeping the Go constant and migration SQL byte-identical.
- Keep the package clause as `package reachabilitystore`; callers import the
  `storage/postgres/code/reachability` path without an alias.
- Never import the parent `postgres` package from non-test code here.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/code/reachability/... -count=1
go vet ./internal/storage/postgres/code/reachability/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
