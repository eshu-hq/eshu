# Semantic-entity projector intents

## Purpose

This package decides whether one `content_entity` fact carries enough
semantic structure to be worth materializing, and builds the
`semantic_entity_materialization` reducer intent when it does. Admission is
entity type first — `Annotation`, `Typedef`, `TypeAlias`, `Component`,
`Module`, `ImplBlock`, `Protocol`, `ProtocolImplementation` are semantic on
their own — then a set of per-language predicates that admit callables and
language-specific shapes carrying real metadata. A plain Go `func` with no
docstring, receiver, decorator or type parameter produces no intent. A bare
TypeScript ES module does not behave that way: `Module` is in the closed
`semanticEntityReducerTypes` set, and the per-language predicates run only for
types absent from that set (`entity_intents.go:61`), so a module short-circuits
past `isTypeScriptModuleSemanticEntity` and is admitted unconditionally. That
is the preserved pre-extraction behaviour.

## Ownership boundary

The package owns trigger selection and the reducer-intent value for this one
domain. Root `internal/projector` owns the per-fact loop that calls it,
scope-generation boundary and schema-version validation, intent ordering,
queue writes, retries, and telemetry. The reducer's
`DomainSemanticEntityMaterialization` handler
(`go/internal/reducer/semanticentity/materialization*.go`) owns the entity
rows and graph writes; it re-applies its own language predicates, some of
which share names with the ones here
(`isElixirModuleAttributeSemanticEntity`,
`isTypeScriptJSXComponentTypeAssertionSemanticEntity`) — those are separate
implementations in a separate package, not shared code.

## Exported surface

- `BuildSemanticEntityReducerIntent` builds the
  `semantic_entity_materialization` intent for a single `content_entity`
  fact.

That is the whole exported surface: one function, no exported types. See
`doc.go` for the godoc contract.

## Dependencies

The builder depends on `internal/facts` for the envelope,
`internal/projector/intent` for `ReducerIntent`, and `internal/reducer` for
the domain constant. It must not import the root `projector` package — root
imports this package to dispatch to it, so the reverse import would cycle.

Unlike the scope-generation families it does **not** take an
`intent.FactLookup`: root calls it once per input fact from
`buildProjection`, so its input is a single `facts.Envelope`.

Payload reads go through package-local helpers in `payload.go`.
`payloadString`/`asString` are a copy of root's `payload.go` helpers of the
same name (kept in sync by hand, since the import direction is closed); the
`payloadMetadata*` readers and `payloadStringSlice` moved here wholesale
because root had no other caller for them.

## Telemetry

No-Observability-Change: this package emits no signal directly. Root intent
enqueue remains covered by `eshu_dp_reducer_intents_enqueued_total` and
`eshu_dp_projector_run_duration_seconds`; the reducer handler that consumes
the intent keeps `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. Moving the pure builder adds no
queue, storage, graph, span, metric, or log boundary.

## Gotchas / invariants

- **This builder runs per fact, not per scope generation.** Every sibling
  family under `internal/projector` returns at most one intent per
  generation from `appendScopeGenerationReducerIntents`. This one is called
  from root's `buildProjection` per-fact loop (`../runtime.go`) and can
  return an intent for many facts in the same generation. They all share the
  `repo:<repo_id>` entity key, so root's deterministic sort and the
  reducer's per-key claim collapse them into one unit of work — but
  `result.Intents.Count` does NOT count each one: it is the enqueue
    INSERT's `RowsAffected`, not `len(intents)` (`../runtime.go:59`, #5593).
    Accepted facts sharing a repository yield the same work-item ID, so
    `ON CONFLICT DO NOTHING` collapses them and the count is normally one.
- `SourceSystem` is the raw `fact.SourceRef.SourceSystem`, **not** the
  two-tier trimmed `projectorintent.SourceSystem` fallback the
  scope-generation families use. That difference is the preserved
  pre-extraction behavior; do not "fix" it to match the siblings without
  deciding what a fact with an empty `SourceRef` should be labelled.
- A blank or whitespace-only `repo_id` rejects the fact. The entity key is
  the repository acceptance unit, so there is nothing to key on without it.
- `entity_type` is read from the flat payload only. The `entity_kind`
  fallback that `projector.buildContentEntityRecord` and the
  `crossplanesatisfiedby` trigger use is **not** applied here — this
  predicate has always read `entity_type` alone.
- The `payloadMetadata*` readers check the flat payload key first and fall
  back to the nested `entity_metadata` map, because parser adapters emit
  metadata both ways. `payloadMetadataBool` accepts only a real `bool`; a
  `"true"` string is not coerced, unlike root's `payloadBoolPtr`.
- `semanticEntityFactKind` duplicates root's `FactKindParsedEntityObserved`
  (`"content_entity"`, `../stage_facts.go`) as a literal rather than an
  import, because root imports this package and the reverse direction would
  cycle.

## Related docs

- [Projector architecture](../README.md)
- [Intent contract](../intent/README.md)
- [Elixir language support](../../../../docs/public/languages/elixir.md) —
  cites this package's Elixir tests as parity evidence
- [Package restructure](../../../../docs/internal/design/package-restructure.md)

No-Regression Evidence: this extraction moves one builder and its four test
files without changing the trigger or the intent value. The domain
(`DomainSemanticEntityMaterialization`), the `repo:<repo_id>` entity key, the
`semantic entity follow-up for <entity_type>` reason string, the `FactID`
anchor, and the raw `SourceRef.SourceSystem` label are unchanged from the base
commit; the function is called at the same position in root's per-fact loop,
still after `buildRepositoryRefs` and before `buildReducerIntent`. The
per-fact call site is in `../runtime.go`, not the
`scope_generation_intents.go` fan-out, so the ordered fan-out is untouched:
`appendScopeGenerationReducerIntents` has 44 builder probes on `origin/main`
and 44 on this branch, and `TestReducerIntentProbeCountMatchesDocumentedCount`
still passes against the unchanged
`documentedReducerIntentProbeCount`. Verified with
`go build ./...` (rc=0), `go vet ./internal/projector/...` (rc=0),
`go test ./internal/projector/... -count=1` (rc=0, 30 packages),
`go test ./internal/mcp/ -run TestRouteServesDataRegistry -count=1` (rc=0,
8 `=== RUN` lines), and `bash scripts/verify-dirgate.sh --all` (rc=0).

The four moved test files are the builder-level coverage and they moved
verbatim apart from the package clause and the now-exported call. Root keeps
the integration coverage that goes through the dispatcher:
`TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationTypedefTypeAliasComponentAndFunction`
and `TestBuildReducerIntentQueuesJavaScriptCallableSemanticEntities` in
`../runtime_test.go`, plus the `#4854` mutation-safety and clone-vs-borrow
equivalence tests in `../runtime_clone_removal_test.go`, which exercise this
builder through `buildProjection`. The root fan-out parity fixture
(`../scope_generation_intents_fanout_parity_test.go`) does NOT mention this
domain and never did — it only covers `appendScopeGenerationReducerIntents`.

No-Observability-Change: no metric, span, log, or quarantine counter is added,
moved, or renamed by this extraction. Root assembly and
`eshu_dp_reducer_intents_enqueued_total` are untouched, and the two new
non-test files under this package emit no signal of their own —
`payload.go` is unexported map and scalar readers that perform no I/O, and
`entity_intents.go` is a pure trigger-and-value builder. The
telemetry-coverage rows for both were written from what the files contain.
