# internal/reducer/searchvector

## Purpose

Runs the reducer's search-vector build sweep: a side-runner goroutine that
builds derived embedding-vector rows for active curated search documents,
feeding the `mode=semantic`/`mode=hybrid` search read path (issue #6061;
originally issues #4233, #4430, #4673, #4885). It moved out of the reducer
root as a self-contained family — three files, no shared coupling with any
other still-in-root domain beyond the `Service` struct field that starts it.

`SearchVectorBuildRunner.Run` sweeps for pending vector builds until its
context is canceled. Each bounded sweep lists pending active scopes, builds
vectors in bounded per-scope document batches (or one batched call across all
pending scopes when the wired builder supports it), and continues through
independent scope failures, returning a joined error. The runner writes no
graph truth, but it is not limited to one side effect: besides whatever its
`SearchVectorBuilder` port implementation persists, a wired `ScopeState`
persists build fences, document cursors, and readiness (the #4233 lifecycle),
and a wired `ReadyPublisher` persists the `search_vector_ready` watermark.
Both are wired in production — see Ownership boundary and Telemetry below.

## Ownership boundary

**Owns:** `SearchVectorBuildRunner` and its sweep/backoff loop, the
`SearchVectorBuildPendingLister`/`SearchVectorBuilder`/
`SearchVectorBatchBuilder`/`SearchVectorBuildReadyPublisher`/
`SearchVectorScopeStateManager` consumer ports, the sweep's request/result/
config/identity value types, and the sweep's structured logging and
`eshu_dp_search_vector_build_phase_seconds` phase-timing metric emission.

**Does not own:** the actual vector build (`internal/searchvector.Builder`,
a sibling top-level package with a name collision this package's own
`go/cmd/reducer` caller resolves with an import alias — see Gotchas), the
Postgres-backed vector-scope-state store, ready-watermark store, or
`EshuSearchDocument`/curated-search-read-model projection (those live in
`internal/storage/postgres` and `internal/reducer/eshusearch` respectively),
and the query-side freshness read (`internal/query/semanticsearch`). This
package only runs the sweep loop and defines the ports those other packages
implement or consume.

## Exported surface

| symbol | what it is |
|---|---|
| `SearchVectorBuildRunner` | the side-runner; `Run`/`RunOnce` drive the sweep loop |
| `SearchVectorBuildRunnerConfig` | poll interval, scope/document limits, provider/embedding identity |
| `SearchVectorBuildIdentity` | the (provider profile, source class, embedding model, vector index version) tuple that scopes pending discovery, builds, and the ready signal |
| `SearchVectorBuildPendingScope` / `SearchVectorBuildPendingRequest` / `SearchVectorBuildPendingLister` | pending-scope discovery port |
| `SearchVectorBuildRequest` / `SearchVectorBuildResult` / `SearchVectorBuilder` / `SearchVectorBatchBuilder` | per-scope and batched build ports |
| `SearchVectorBuildReadyPublisher` | publishes the `search_vector_ready` completion signal |
| `SearchVectorScopeStateManager` / `SearchVectorBuildScopeProgress` | the #4233 per-scope vector-scope-state lifecycle port |
| `SearchVectorBuildRunnerResult` | one bounded sweep's summary (built/finalized counts, phase durations) |
| `DomainSearchVectorBuild` | the split-timing histogram's domain label; re-exported from `reducer/contract`, since this sweep has no intent-dispatch `Domain` of its own |
| `SearchVectorBuildPhaseSchedulingWait`/`QueryLoad`/`EmbedBuild`/`WriteUpsert` | the closed set of four phases recorded on the phase-duration histogram |

See `doc.go` for the full godoc-rendered contract, including the backoff and
ready-publish gating rules.

## Dependencies

- `github.com/eshu-hq/eshu/go/internal/reducer/contract` — `DomainSearchVectorBuild`.
- `github.com/eshu-hq/eshu/go/internal/telemetry` — `Instruments.SearchVectorBuildPhaseDuration`, `AttrDomain`, `AttrWritePhase`.
- `github.com/eshu-hq/eshu/go/pkg/log` — `log.Err`/`log.FailureClass` structured-log helpers.
- `go.opentelemetry.io/otel/metric` — the option types the phase-duration
  histogram is recorded with (`search_vector_build_runner_log.go`).

It imports nothing storage- or embedding-specific and never imports the
reducer root. `go/cmd/reducer` adapts the concrete
`internal/searchvector.Builder` and `internal/storage/postgres` stores to
this package's ports.

## Telemetry

- `eshu_dp_search_vector_build_phase_seconds` — histogram, labeled by
  `domain` (always `search_vector_build`) and `write_phase`
  (`scheduling_wait`/`query_load`/`embed_build`/`write_upsert`); emitted by
  `recordPhaseMetrics`, called inline (not deferred) at the end of every
  `RunOnce` code path that reaches per-scope work. Three early returns in
  `RunOnce` skip it: config validation failure, a
  `ListPendingSearchVectorScopes` failure, and — when `ScopeState` is wired —
  a `BeginBuilding` failure. A pending-list or `BeginBuilding` failure inside
  `Run`'s sweep loop still surfaces as the `"search vector build sweep
  failed"` error log (`Run` calls `logFailure` on any `RunOnce` error), just
  with no phase sample for that call. A validation failure is different: it
  fails `Run` at startup, before the loop and before any log call, so it
  produces neither a phase sample nor that failure log — only the returned
  error itself. Either way, an operator diagnosing one of these three
  failures cannot lean on the histogram.
- Structured logs at `phase="reduction"`: `"search vector build sweep
  completed"` (per-sweep counts and split-timing seconds), `"search vector
  build sweep failed"` (`failure_class=search_vector_build_error`), `"search
  vector build ready signal publish failed"`
  (`failure_class=search_vector_ready_publish_error`), and `"search vector
  build sweep made no progress; backing off"`
  (`stall_reason=no_durable_output`).

Full contract: `docs/public/observability/telemetry-coverage.md` rows for
this package's three files.

## Gotchas / invariants

- **Name collision with `internal/searchvector`.** That sibling top-level
  package (not under `internal/reducer`) implements the actual vector
  builder this package's `SearchVectorBuilder` port wraps. `go/cmd/reducer`
  imports both in `search_vector_build_wiring.go` and aliases this package's
  import as `reducersearchvector` to disambiguate.
- **Never import the reducer root.** This package is a leaf below
  `internal/reducer`; the root imports it (through the compatibility aliases
  in `search_vector_build_compat.go`) for the `Service.SearchVectorBuildRunner`
  field, never the reverse.
- **Backoff is load-bearing, not cosmetic.** `searchVectorBuildSweepMadeProgress`
  gates the immediate-continue vs. poll-interval-backoff branch in `Run`. A
  regression here reopens the #4885 hot loop: a never-draining pending set
  (e.g. every scope has documents but the builder can't advance them) pins
  Postgres 24/7 instead of resting on the poll interval.
- **The ready-publish re-check runs AFTER the build, not before.** Gating
  `PublishSearchVectorReady` on the pre-build `PendingScopes` count misses
  the sweep that drains the LAST pending scopes (issue #4673 review fix).
- **`BeginBuilding`'s fence is a CAS token, not a lock.** Every subsequent
  scope-state call (`AdvanceDocumentCursor`, `ResetDocumentCursor`,
  `FinalizeReady`) carries the fence from `BeginBuilding` so a delayed worker
  cannot overwrite a newer build after ownership changes; a `false`
  CAS-rejected result is logged and skipped, never retried inline.

## Related docs

- `go/internal/reducer/search-and-runtime-projections.md` — the `SearchVectorBuildRunner`
  and `search_vector_ready` completion-signal sections, including the
  cross-package flow into `internal/query/semanticsearch`.
- `docs/public/observability/telemetry-coverage.md` — this package's
  telemetry rows.
- `go/internal/reducer/evidence-5063-vector-tail-page-budget.md` and
  `go/internal/reducer/evidence-4430-search-vector-sweep-batching.md` —
  performance evidence for the tail-scope document-limit budget and the
  split-timing phase metric this package emits.
