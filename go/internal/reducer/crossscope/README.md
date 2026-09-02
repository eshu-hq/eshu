# Cross-scope producer readiness

## Purpose

`crossscope` owns the #5709 cross-scope producer-readiness floor and the
dependency catalog it reads: the correctness gate that stops a consumer
reducer domain from committing a durable "no correlation" answer when the
producer domain it depends on, in a different ingestion scope, has not yet
activated for the relevant generation.

It exists because of the same import-direction constraint that moved
`payloadcore`, `factload`, and `factwrite` below the reducer root: as families
move out of the root into their own subpackages, the root imports them to
construct their handlers, so a family cannot import the root back without
creating a cycle. This particular tier could not simply move alongside one
family, because it is not owned by one family (issue #6061, epic #6053).

## Ownership boundary

This package owns:

- The readiness floor: `ProducerReadiness` (the port), `ProducerReadinessByDomain`,
  `ProducerReadinessSignal`, `CheckProducerReadinessBeforeLoad`,
  `UnreadyProducers`, `SingleProducerResolvedCounts`, `ReadinessCycleAnchor`,
  `LogProducerNotReadyDefer`, and `ProducerReadinessMaxWait`.
- The readiness error: `ProducerNotReadyError`, `NewProducerNotReadyError`, and
  `ProducerNotReadyFailureClass`.
- The dependency catalog: the unexported `dependencyCatalog`, plus
  `ConsumerDomains`, `CompletionEdge`, `CompletionEdges`, and
  `DependenciesForRegistration`.

It owns no domain knowledge beyond the catalog's own producer/consumer
declarations, no fact decoding, no writer, and no queue or Postgres access. The
parent reducer package still owns registry composition, runtime and queue
execution, and the two handlers that call this floor
(`CICDRunCorrelationHandler` in `ci_cd_run_correlation.go`,
`SupplyChainImpactHandler` in `supply_chain_impact_evidence_load.go`).

## Why this is a shared tier, not a family helper

The catalog and the floor were moved together, as one package, because
`checkCrossScopeProducerReadinessBeforeLoad` calls
`crossScopeDependenciesForRegistration` directly; splitting the catalog into
the reducer root while the floor moved out would recreate the same import
cycle the move exists to avoid (the root already needs to import this package
for the compatibility forwarders it keeps).

The tier is genuinely shared, not merely convenient to share: both
`ci_cd_run_correlation` and `supply_chain_impact` are registered consumers in
the catalog, and `LogProducerNotReadyDefer` is called from both
`ci_cd_run_correlation.go` and `supply_chain_impact_evidence_load.go`. Moving
it into either family's subpackage would leave the other needing to import a
sibling family, which the restructure forbids — families import shared-core
tiers, never each other.

`cross_scope_completion_runner.go` stayed in the reducer root. It is the
service-side lease/queue runner that schedules a consumer's replay after a
producer's completion ACK (wired through `service.go`); it reads
`CompletionEdges` but is orchestration plumbing, not something either
`ci_cd_run_correlation` or `supply_chain_impact` needs directly, so it did not
belong in this move.

## Ordering assumption

`CheckProducerReadinessBeforeLoad` MUST be called BEFORE the consumer's own
cross-scope load, never after. Sampling readiness after the load reopens the
exact race the floor exists to close: a producer generation that activates in
the window between the load and a post-load check would make the store report
"ready" about a snapshot the load already took without it, and the handler
would durably write an empty correlation that no later event repairs. The
reverse race — a producer activating between this check and the load — is
benign, because the load then reads fresher data than the signal assumed,
which can only add evidence.

The floor's own deferral is bounded by elapsed time
(`ProducerReadinessMaxWait`, 30 minutes), anchored on `intent.CycleStartedAt`
falling back to `intent.EnqueuedAt`, and it MUST NOT be rebuilt as an
attempt-count comparison: `ProducerNotReadyFailureClass` is enrolled in
`nonCountingReducerRetryFailureClasses`
(`go/internal/storage/postgres/reducer_queue_readiness_sql.go`), which freezes
`fact_work_items.attempt_count` for exactly this class, so a count-based bound
would read the same value forever and never fire. See the doc comments on
`ProducerReadinessMaxWait` and `ReadinessCycleAnchor` for the two prior
incidents (the #5875 P1 ordering bug and the sibling AWS gate's attempt-count
bound) this repeats the guard against.

## Telemetry

`LogProducerNotReadyDefer` emits the same structured log line, at the same
level, with the same field names, as before the move: `log.Domain`,
`log.ScopeID`, `log.GenerationID` (from `go/pkg/log`, which set the
`telemetry.LogKeyDomain` / `LogKeyScopeID` / `LogKeyGenerationID` keys — those
constants are untouched by this move), plus `producer_domains`, `max_wait`, and
`elapsed_since_cycle_start`, on the message `"cross-scope consumer deferred:
producer scopes have not activated"`. No metric instrument, counter, span, or
Postgres operation is associated with this floor; `cmd/golden-corpus-gate/drains.go`
matches the literal string `"cross_scope_producer_not_ready"`, and that string
value (`ProducerNotReadyFailureClass`) is copied verbatim, unchanged.

## Compatibility

The reducer root keeps `cross_scope_readiness_compat.go` as the transitional
compatibility surface: unexported function-statement forwarders and type
aliases for every symbol this package took over, under their EXACT original
root spelling, so none of the 92-plus existing call sites in
`ci_cd_run_correlation.go`, `supply_chain_impact_evidence_load.go`,
`registry_additive_domains.go`, and the storage layer's
`internal/storage/postgres/cross_scope_completion_fanout.go` and
`cross_scope_producer_readiness.go` (which reference `reducer.CrossScopeCompletionEdges`,
`reducer.CrossScopeProducerReadinessByDomain`, and
`reducer.CrossScopeProducerNotReadyFailureClass`) changed. They are function
statements, never function-valued variables, so they stay inlinable on the
reducer write path; forwarders are deleted once their last root caller has
moved into a family subpackage.

Two fields could not stay unexported across the package boundary and needed a
name, not a caller behavior, change: `ProducerReadinessSignal.ProducerDomains`
and `ProducerNotReadyError.ProducerDomains` are exported (capitalized) because
`ci_cd_run_correlation.go` and a supply-chain-impact test read them directly
by field access, which an unexported field cannot support across packages
even through a type alias. `ci_cd_run_correlation.go:173` and
`supply_chain_impact_cross_scope_readiness_test.go` were repointed at the
capitalized name; every other call site is unchanged.

## No-Regression / No-Observability-Change Evidence

No-Regression Evidence: #6061 moves the cross-scope producer-readiness floor
(`cross_scope_readiness.go`, `cross_scope_readiness_floor.go`) and the
dependency catalog (`cross_scope_dependencies.go`) out of the reducer root
into this package, cut-paste with identifiers capitalized and doc comments
rewritten, and leaves compatibility forwarders in
`cross_scope_readiness_compat.go` so every existing root caller keeps
compiling against the identical behavior. Two struct fields needed to become
exported (see Compatibility above); their two call sites were updated
accordingly. Measured on the branch, from `go/`:
`go build ./...` exited 0; `go vet ./...` (which also compiles test files, so
it catches a moved test fixture breaking a sibling package) exited 0;
`go test ./internal/reducer/... -count=1` exited 0 across all 16 reducer
subpackages including the new `crossscope`; `go test ./cmd/reducer
./internal/storage/postgres ./internal/query -count=1` exited 0, which proves
the storage layer's `reducer.CrossScopeCompletionEdges`,
`reducer.CrossScopeProducerReadinessByDomain`, and
`reducer.CrossScopeProducerNotReadyFailureClass` call sites still resolve and
behave the same; `go test ./internal/ifa/materializededges/... -count=1`
exited 0.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
Postgres operation, runtime setting, metric instrument, or metric label. The
one log line this floor emits (`LogProducerNotReadyDefer`) keeps its exact
message text, level, and field names (`log.Domain`, `log.ScopeID`,
`log.GenerationID`, `producer_domains`, `max_wait`,
`elapsed_since_cycle_start`); the `ProducerNotReadyFailureClass` string value
`cross_scope_producer_not_ready` that `cmd/golden-corpus-gate/drains.go` and
`internal/storage/postgres/reducer_queue_readiness_sql.go` depend on is
copied verbatim, unchanged.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)
