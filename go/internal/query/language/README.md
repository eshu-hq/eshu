# Language Query

## Purpose

The language-specific entity query route: `POST /api/v0/code/language-query`.
Looks up entities of one graph label or content entity type in one language,
across three entity-type families -- graph-backed, graph-first-content, and
content-only -- each with its own capability-gated, grant-bound dispatch
branch.

## Ownership boundary

Owns the `Handler` struct, its HTTP dispatch and both Cypher builders and
content-store reads it calls, the language name/spelling registry, and the
truth-basis-to-reason and truth-basis-to-source_backend mappings this route
reports. Does not own the repository-selector resolution or the language-query
grant type (`codequery`), the content-model types or capability registry
(`querycontract`), the semantic-summary attachment
(`entitysemantics`), or the shared semantic-metadata Cypher fragment
(`rows`) -- those are separate leaves this package calls into.
Does not own `*ContentReader` (package query, a later lane's family): this
package reaches it only through the `querycontract.ContentStore` and
`taxonomy.LanguageEntityContentSearcher` port interfaces.

## Layout

- `handler.go` -- `Handler`, `Mount`, `handleLanguageQuery` and its
  entity-type-family dispatch (graph-backed, graph-first-content, content-only),
  the three `queryByLanguage*` / `queryGraphFirstContentByLanguage*` entry
  points, and the route capability and reason constants. The graph-backed
  branch's single build-and-run now calls `languageQueryGraphRows` in
  `directory.go` rather than building the statement here.
- `directory.go` -- the graph half of one language query
  (`languageQueryGraphRows`, which routes the `Directory` label away from the
  shared build-and-run) and everything the `Directory` branch needs that its
  statement can no longer supply on its own (#6541): the repository-id list the
  statement UNWINDs (`directoryRepositoryIDsForGrant`, `allRepositoryIDs`), the
  read that fills `repo_name` (`directoryRepositoryNames`), the whole-result
  re-sort and truncate (`sortAndTruncateDirectoryRows`), and the read's debug
  log (`logDirectoryRead`).
- `handler_tracing.go` -- the route's own span seam (`languageHandlerTracer`,
  `startQueryHandlerSpan`), the same shape the other leaves keep in their
  `handler_tracing.go`.
- `handler_write.go` -- the success and unsupported-capability response
  writers.
- `cypher.go` -- the four Cypher builders (`buildRepositoryCypher`,
  `buildDirectoryCypher`, `buildFileCypher`,
  `buildEntityCypherWithSemanticFilter`) and their dispatcher
  (`BuildCypherWithSemanticFilter`), plus `SupportedLanguages` and
  `SupportedEntityTypes`. `buildDirectoryCypher` is the one builder that binds
  no `Repository`; the contract its exported dispatcher carries for a caller
  that must supply the resolved repository-id list is on
  `BuildCypherWithSemanticFilter`'s doc comment. Its indexed Directory owner
  anchor and File owner pattern must match the same repository id so a stale
  cross-repository CONTAINS edge cannot change the scoped file count (#6703).
- `entities.go` -- `buildLanguageResult` (graph row to response shape),
  the three entity-type-family maps, and the unsupported-entity-type response
  writer.
- `metadata.go` -- `EntitySearch`, the content-metadata enrichment merge
  (`enrichLanguageResultsWithContentMetadata`), the repository-scoped merge
  key, and `searchLanguageEntities` (the grant-bound content-store dispatch
  `codequery/relationships/story.SearchEntitiesForGrant` cross-checks).
- `reasons.go` -- the per-branch truth-basis-to-reason mappings, the
  truth-basis-to-source_backend mapping (`SourceBackendForTruthBasis`), and
  the empty-grant response writer.
- `registry.go` -- the supported-language set, language canonicalization, and
  the graph `language` property spelling list a Cypher builder binds.
- `graph_configured.go` -- the graph-only-entity-unavailable sentinel error.
- Test files -- this package's own tests, several moved in verbatim from
  root (see Move evidence); `entity_search_dispatch_test.go`,
  `cypher_shipped_text_test.go`, `repository_match_key_test.go` construct
  fixtures hoisted to `testutil` (see AGENTS.md).

## Move evidence

The family moved here from the query root (`language_queries.go` and its
`language_query_*.go`/`language_registry.go` siblings, #6642, split off the
#6060 lane A restructure); the non-test files are destuttered
(`language_queries.go` -> `handler.go`, etc.) and `LanguageQueryHandler`
renamed to `Handler` at its declaration, with every method and field name
otherwise unchanged. Root keeps every pre-move exported spelling through a
stanza in the new `language_alias.go`: the `LanguageQueryHandler` type alias,
the `languageEntitySearch` type alias, the `normalizedLanguageVariants`,
`SupportedEntityTypes`, `SupportedLanguages`, and
`sourceBackendForTruthBasis` forwarders, and the `*ContentReader` port
compile-time pin. The four live-tagged NornicDB grant tests in root import
this package and call `BuildCypherWithSemanticFilter` directly.

No-Regression Evidence: baseline `origin/main` vs this branch -- the Cypher
text in `cypher.go` is byte-identical to the pre-move source (verified by the
`cypher_shipped_text_test.go` frozen-text tests that moved with it and by the
queryplan `source_sha256` re-pin for
`(*Handler).queryByLanguageWithSemanticFilter` in
`internal/queryplan/testdata/query-source-coverage.yaml`); `go test
./internal/query/...` and `go test ./internal/query/language/` pass, and their
combined test-name union equals the pre-move `go test ./internal/query/
-list '.*'` list exactly (2868 names, no duplicate, no drop); the
route-serves-data registry entries citing this route's file paths point at the
new files with unchanged evidence markers.

No-Observability-Change: the span this route emits keeps its name
(`telemetry.SpanQueryLanguageQuery`) and `http.route`/`eshu.capability`
attributes. `languageHandlerTracer` is this package's own package-local
tracer var (mirroring `incidentHandlerTracer` in
`go/internal/query/incident/handler.go`), seeded from
`tracing.HandlerTracer()`; `span_test.go` (moved with the family, since the
tracer it swaps is now package-local) proves the handler still emits exactly
one span with those attributes.

## Related docs

- `docs/public/reference/language-query-dsl.md`
- `docs/internal/evidence/6546-language-query-extension-filter.md`
- `docs/internal/evidence/5167-code-family-batch-2.md`
- `go/internal/query/read-models.md`
