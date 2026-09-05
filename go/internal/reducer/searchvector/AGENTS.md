# Agent instructions: internal/reducer/searchvector

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The reducer's search-vector build sweep: `SearchVectorBuildRunner`, its
consumer ports, and its request/result/config/identity value types (issue
#6061; originally issues #4233, #4430, #4673, #4885). It is a side-runner
goroutine, not an intent-dispatch reducer domain — see the README's
Ownership boundary section for exactly what it does and does not own.

The runner writes no graph truth, but do not read that as "only the builder
persists something": when `ScopeState` and `ReadyPublisher` are wired — both
are wired in production — `RunOnce` also persists build fences, document
cursors, and readiness through `ScopeState`, and the `search_vector_ready`
watermark through `ReadyPublisher`. Treat all three ports as real persistence
paths when reasoning about what a change here can break.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/searchvector/README.md`
- `go/internal/reducer/search-and-runtime-projections.md` (the
  `SearchVectorBuildRunner` and `search_vector_ready` sections)

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (through
  `search_vector_build_compat.go`'s type aliases) for the
  `Service.SearchVectorBuildRunner` field and its own
  `TestServiceStartsSearchVectorBuildRunner` wiring proof, never the reverse.
- **`searchVectorBuildSweepMadeProgress` gates the hot-loop guard.** `Run`
  backs off on the poll interval instead of re-looping immediately when a
  sweep selected pending scopes but produced zero finalized/document/vector/
  disabled output. Do not weaken this to "any successful RunOnce call" — a
  sweep can succeed (no error) while doing nothing durable, which is exactly
  the never-draining condition #4885 fixed.
- **The ready-publish re-check (`publishReadyIfCaughtUp`) queries pending
  scopes AFTER the build, not before.** Do not fold this into the pre-build
  `RunOnce` listing — that misses the sweep that drains the LAST pending
  scopes (#4673 review fix): non-zero pre-build count, truly caught-up
  post-build state.
- **Every scope-state call after `BeginBuilding` carries its returned
  fence.** `AdvanceDocumentCursor`/`ResetDocumentCursor`/`FinalizeReady` all
  take the fence so a delayed worker's write is rejected once a newer build
  supersedes it (a `false` return, not an error — log and skip, do not retry
  inline against the caller's context).
- **`SearchVectorBuildIdentity` is the freshness/build-scoping key, never
  bypass it.** Pending discovery, build requests, and the ready signal are
  all keyed by (provider profile, source class, embedding model, vector
  index version); mixing identities would serve stale-under-new-config
  results as fresh during a provider/model/index-version rollout.
- **`internal/searchvector` (no `reducer/` prefix) is a different package**
  that implements the concrete builder this package's `SearchVectorBuilder`
  port wraps. `go/cmd/reducer/search_vector_build_wiring.go` imports both and
  aliases this package as `reducersearchvector` — do not "fix" that alias by
  renaming either package; the collision is real and intentional (two
  packages, two concerns).

## Common changes

- **New build phase or metric label:** extend the closed
  `SearchVectorBuildPhase*` constant set in `search_vector_build_runner.go`
  and the `record(...)` calls in `recordPhaseMetrics`
  (`search_vector_build_runner_log.go`) together; update
  `docs/public/observability/telemetry-coverage.md`.
- **New scope-state transition:** add the method to
  `SearchVectorScopeStateManager` and thread it through
  `finalizeVectorScopeStates` (`search_vector_build_runner_scope_state.go`);
  update `go/internal/storage/postgres`'s
  `EshuSearchVectorScopeStateStore` implementation and its adapter in
  `go/cmd/reducer/search_vector_build_wiring.go` in the same change.
- **New pending-request or build-request field:** add it to both
  `SearchVectorBuildPendingRequest`/`SearchVectorBuildRequest` here and the
  mirrored `postgres.EshuSearchVectorPendingRequest`/
  `searchvector.BuildRequest` shapes the adapters translate between — these
  are deliberately duplicated across the package boundary rather than
  imported.

## Failure modes to watch for

- A sweep that reports `PendingScopes > 0` with `FinalizedScopes == 0`,
  `DocumentCount == 0`, `VectorCount == 0`, and `DisabledCount == 0` every
  cycle is the #4885 stall signature — check the pending lister/builder pair
  for a mismatch, not the runner loop.
- A `search_vector_ready` publish that never happens despite an apparently
  caught-up corpus usually means `ReadyPublisher` is nil (legacy/local wiring)
  or the post-build re-check is racing a concurrent write that re-populates
  the pending set; it does not mean this package's gating logic is wrong.

## What not to change without discussion

- The four-phase `SearchVectorBuildPhase*` set is a closed, bounded label
  set for the `eshu_dp_search_vector_build_phase_seconds` histogram. This
  package's `recordPhaseMetrics` is that histogram's only producer, so a fifth
  phase widens its label cardinality outright with no second call site to
  reconcile against. Keep the set bounded, and update
  `docs/public/observability/telemetry-coverage.md` in the same change.
- The `SearchVectorBuildIdentity` field set (provider profile, source class,
  embedding model, vector index version) is mirrored in
  `postgres.EshuSearchVectorBuildIdentity` and
  `query/semanticsearch.SearchVectorBuildIdentity`. Changing it here without
  changing both mirrors breaks the identity-scoped freshness contract
  silently (compiles, wrong behavior).
