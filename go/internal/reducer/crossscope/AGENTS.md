# Cross-scope producer-readiness package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/crossscope/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- This package must remain a leaf below `internal/reducer`. Never import the
  parent reducer package or a domain-family subpackage. Budget:
  `internal/reducer/contract`, `internal/reducer/factload`,
  `github.com/eshu-hq/eshu/go/pkg/log`, and the standard library.
- `CheckProducerReadinessBeforeLoad` MUST be called BEFORE the consumer's
  cross-scope load, never after. Sampling after the load reopens the #5875 P1
  ordering bug: a producer activating in the window between the load and a
  post-load check would make the store answer "ready" about a snapshot that
  predates the activation, and the handler durably writes a wrong answer
  nothing later repairs.
- `ProducerReadinessMaxWait` (30 minutes) bounds the deferral by ELAPSED TIME,
  anchored via `ReadinessCycleAnchor`. It must NEVER be rebuilt as an
  attempt-count comparison: `ProducerNotReadyFailureClass` freezes
  `fact_work_items.attempt_count` (see
  `internal/storage/postgres/reducer_queue_readiness_sql.go`'s
  `nonCountingReducerRetryFailureClasses`), so a count-based bound reads the
  same frozen value forever and can never fire. The sibling AWS gate shipped
  that mistake first.
- `UnreadyProducers` and `SingleProducerResolvedCounts` must stay PER PRODUCER
  DOMAIN, never a single aggregate bool/count pair. #6093 found
  `supply_chain_impact` committing findings with no deployment context because
  two envelopes from one producer disarmed the floor for a second producer
  that had published nothing — an aggregate cannot tell those cases apart.
- `dependencyCatalog` is the single source of truth for producer/consumer
  edges. `ConsumerDomains` and `CompletionEdges` derive from it and must never
  hard-code a parallel list; the storage layer's completion fanout schema
  test cross-checks against `CompletionEdges` directly.
- `ProducerReadinessSignal.ProducerDomains` and
  `ProducerNotReadyError.ProducerDomains` are exported for a reason: the
  reducer root reads them by field access across the package boundary (see
  the README's Compatibility section). Do not re-unexport them without also
  fixing `ci_cd_run_correlation.go` and
  `supply_chain_impact_cross_scope_readiness_test.go`.

## Common changes

Adding a third cross-scope consumer: add its entry to `dependencyCatalog` in
`dependencies.go`, wire its handler to call `CheckProducerReadinessBeforeLoad`
/ `UnreadyProducers` / `LogProducerNotReadyDefer` the same way
`ci_cd_run_correlation.go` and `supply_chain_impact_evidence_load.go` do, and
extend `TestCompletionEdgesExposeCatalogExactly` and
`TestDependencyCatalogIsValid`. Being in the catalog does not gate a
consumer by itself — each handler must opt in by calling the floor helpers.

Adding a root forwarder: match the existing shape in
`cross_scope_readiness_compat.go` — a function statement (never a
function-valued variable, this sits on the reducer write path and a func var
cannot be inlined) or a type alias, named EXACTLY the symbol's old root
spelling, so no existing caller changes.

## Failure modes

- Splitting the readiness floor from the dependency catalog into separate
  root/leaf halves recreates an import cycle:
  `CheckProducerReadinessBeforeLoad` calls `DependenciesForRegistration`
  directly, and the reducer root already imports this package for its
  compatibility forwarders.
- Retyping a moved body instead of cut-pasting it silently drops a guard —
  see the `payloadcore` package's own postmortem on this. Compare each moved
  body against the pre-move root file, not just its signature.
- Forgetting to update the two root call sites that read
  `ProducerDomains` by field access breaks the build immediately (Go visibility
  is per-package, not per-name; a type alias does not make an unexported field
  reachable from another package) — that is a compile error, not a silent
  regression, but it is the one place this move required a caller edit rather
  than a pure forwarder.
