# AGENTS.md — semantic-entity projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral intent contract.
4. `../runtime.go` — the per-fact `buildProjection` loop that calls this
   builder. This family is dispatched from there, NOT from
   `../scope_generation_intents.go`.
5. `go/internal/reducer/semanticentity/materialization_helpers.go` for what
   the reducer does with the intent, including its own copies of two
   predicate names used here.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildSemanticEntityReducerIntent` takes a single `facts.Envelope`, not a
  `projectorintent.FactLookup`. It is called once per input fact and may
  return an intent for many facts in one generation.
- Keep the reason string (`"semantic entity follow-up for <entity_type>"`)
  and the `repo:<repo_id>` entity key byte-identical. The entity key is the
  repository acceptance unit the reducer claims on; changing it changes how
  work collapses.
- `SourceSystem` is the raw `fact.SourceRef.SourceSystem`. Do NOT replace it
  with `projectorintent.SourceSystem` — that two-tier trimmed fallback is
  what the scope-generation families use, and swapping it in here would
  change the label for a fact with an empty `SourceRef` but a set
  `CollectorKind`.
- `entity_type` is read from the flat payload only; there is no `entity_kind`
  fallback in this family.
- The `payloadString`/`asString` pair in `payload.go` is a hand-kept copy of
  root's `payload.go` helpers. If root's semantics change, change both —
  nothing enforces the pairing.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Admitting a new entity type.** Add it to `semanticEntityReducerTypes` if
  it is semantic with no metadata check, otherwise add a language predicate.
  Then decide whether the reducer's own language gate in
  `go/internal/reducer/semanticentity/materialization_helpers.go` needs the
  matching addition — admitting here without admitting there enqueues work
  the handler drops.
- **Adding a language predicate.** Add the test to the matching
  `entity_intents_<lang>_test.go`, and add a rejecting case as well: every
  predicate here is a two-sided gate, and a test that only asserts the
  positive passes even if the language check is deleted.
- **Touching a predicate a language-support doc cites.**
  `docs/public/languages/elixir.md` cites
  `entity_intents_test.go::TestBuildSemanticEntityReducerIntentQueuesElixirGuardSemanticEntities`
  and two tests in `entity_intents_elixir_test.go` by path. Renaming a test
  or the file fails `scripts/verify-doc-citations.sh`, which resolves the
  citation file-scoped.

## Failure modes

- **Route-serves-data registry.** `go/internal/mcp/route_serves_data_registry.go`
  cites projector source files BY PATH and reads them for a marker string, so
  a projector-only rename can fail a test in `internal/mcp` with
  `read <path>: no such file` with nothing in this tree pointing at it. At
  extraction time no entry cited any `semantic_entity_intents*` file
  (checked with `rg` over `go/internal/mcp/route_serves_data_registry*.go`),
  and `go test ./internal/mcp/ -run TestRouteServesDataRegistry -count=1`
  passed with 8 `=== RUN` lines. Re-run it before landing any rename here,
  and count the `=== RUN` lines: a `-run` filter that matches nothing still
  exits 0.
- **Root integration tests live outside this directory.** The `Project` and
  `buildProjection` assertions for this domain stayed at root
  (`../runtime_test.go`,
  `TestRuntimeProjectEnqueuesSemanticEntityMaterializationForAnnotationTypedefTypeAliasComponentAndFunction`
  and `TestBuildReducerIntentQueuesJavaScriptCallableSemanticEntities`; and
  `../runtime_clone_removal_test.go`, the `#4854` mutation-safety and
  clone-vs-borrow equivalence tests). A change here can break them without
  touching a file in this directory.
- **The root fan-out parity fixture does not cover this domain, and never
  did.** `../scope_generation_intents_fanout_parity_test.go` only covers
  `appendScopeGenerationReducerIntents`, which this family is not part of.
  Do not treat it as a safety net for a change here.
- **A fact with an empty `SourceRef.SourceSystem`** yields an empty
  `SourceSystem` on the intent. That is the preserved pre-extraction
  behavior, not a bug to patch in passing.

## Anti-patterns

- Do not import the root `projector` package.
- Do not widen the export surface past `BuildSemanticEntityReducerIntent`.
  Every sibling family in this series exports exactly one builder and no
  types.
- Do not add a second payload decode path or a schema-version check here;
  root validates schema version before this builder is called.

## Changes needing ADR review

- Changing `reducer.DomainSemanticEntityMaterialization`, the entity key
  shape, the admitted entity-type set, or the source-system label. All are
  contract surface the reducer handler and the language-support docs assert
  against.

## Verification

Use TDD. Run the focused child tests
(`go test ./internal/projector/semanticentity/ -count=1`), the root projector
package (`go test ./internal/projector/... -count=1`), the focused
`TestRouteServesDataRegistry` run, `scripts/verify-package-docs.sh`,
`scripts/verify-doc-citations.sh` when a cited test or file name changes, and
`scripts/verify-dirgate.sh --all` when a file is added to or removed from the
root projector directory.
