# Evidence: #5333 HANDLES_ROUTE query-proof wrapper

Scope: `go/internal/reducer/handles_route_intents.go` — the sole production
change in PR #5362 is the exported function
`BuildHandlesRouteIntentRowsForQueryProof`.

## Performance

No-Regression Evidence: the sole production change is a thin exported wrapper
(`BuildHandlesRouteIntentRowsForQueryProof`) with exactly one caller — the
cross-package query-proof test — that runs no code path production does not
already run; it adds zero runtime rows, allocations, Cypher, or DB round-trips.
Baseline and after are the same in-memory intent construction.

- Baseline (origin/main): the `HANDLES_ROUTE` intent-materialization pipeline
  (`buildCodeCallProjectionContexts` → `buildCodeEntityIndex` →
  `buildHandlesRouteIntentRows`) is reached only by the production caller
  `symbol_runtime_refresh_intents.go` and the package-local test helper
  `buildHandlesRouteIntentsForTest`.
- After (this PR): `BuildHandlesRouteIntentRowsForQueryProof` is a thin exported
  wrapper with the identical call shape, added solely so the cross-package
  query-proof test (`go/internal/query/code_route_to_caller_java_test.go`) can
  derive its graph-read input from the REAL reducer output instead of inventing
  it. It omits only the queue/worker retract-fence plumbing
  (`markRowsRetractViaRefresh`, `buildRepoWideRetractRefreshIntents`) that is
  irrelevant to the edge payload fields the proof reads.
- Backend/version: backend-agnostic — no Cypher, no graph write, no query text
  changes. The wrapper executes the same in-memory intent construction the
  production path already runs.
- Input shape / worst case: exercised only in tests over the small
  `java_comprehensive/routes/*.java` fixtures.
- Terminal queue/row counts: unchanged — the wrapper has exactly ONE caller
  (the query-proof test); it is never invoked on any runtime, reducer-worker,
  or queue path, so it adds zero runtime rows, allocations, or DB round-trips.
  It is a proof-only entry point, not a behavior change.

## Observability

No-Observability-Change: no new or removed spans, metrics, logs, or status
surfaces. The wrapper is test-only-invoked and carries no telemetry; production
`HANDLES_ROUTE` materialization behavior and its existing instrumentation are
untouched.
