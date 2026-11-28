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
grant type (`codequery`; `codequery.ApplyRepositorySelectorForAccess` also owns
the selector's 503/504, 500 lookup-failure, and 400 answers for this route,
#7626), the content-model types or capability registry
(`querycontract`), the semantic-summary attachment
(`entitysemantics`), or the shared semantic-metadata Cypher fragment
(`rows`) -- those are separate leaves this package calls into.
Does not own `*ContentReader` (package query, a later lane's family): this
package reaches it only through the `querycontract.ContentStore`,
`taxonomy.LanguageEntityContentSearcher`, and
`taxonomy.LanguageEntityMetadataByIDReader` port interfaces.

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
- `metadata_anchored.go` -- the #7247 id-anchored enrichment:
  `contentIDAnchoredLabels` (the labels whose graph id is the content primary
  key), `enrichLanguageResultsByEntityID` (one `entity_id = ANY` lookup keyed
  by the graph rows, merged only into the row with the same id and
  repository), and `recordMetadataEnrichment` (the request-span attributes
  and `eshu_dp_language_query_metadata_rows_total`).
  `metadata_anchored_identity_test.go` pins the label set against the
  projector's real entity extraction.
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

## Id-anchored metadata enrichment (#7247)

For a content-id-anchored label the enrichment no longer reads a second,
independently ordered content page. It reads the graph rows' own content rows
by `entity_id`, with the caller's repository scope bound in the statement.
The row set, order, and non-metadata fields do not change. Rows the page read
missed now carry content metadata, so the truth basis can move from
`authoritative_graph` to `hybrid` on the same rows. Every other label with a
content entity type keeps the page read; a label without one (for example
`TerraformBackend`) never reaches the enrichment.

## Related docs

- `docs/public/reference/language-query-dsl.md`
- (#7247)
- (#6546)
- (#5167)
- `go/internal/query/read-models.md`
