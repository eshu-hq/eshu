# Language Query — Agent Instructions

Scope: `go/internal/query/language/` (package `language`).

## Ownership

This leaf owns the language-specific entity query route (#6642):
`handler.go` (`Handler`, `Mount`, dispatch), `handler_tracing.go` (the span
seam), `handler_write.go` (the response writers), `cypher.go` (the four Cypher
builders and `BuildCypherWithSemanticFilter`), `entities.go`
(`buildLanguageResult`, the entity-type-family maps), `metadata.go`
(`EntitySearch`, the content-metadata enrichment merge,
`searchLanguageEntities`), `reasons.go` (the truth-basis-to-reason and
truth-basis-to-source_backend mappings), `registry.go` (language
canonicalization and spelling), and `graph_configured.go`, plus this
package's own tests, several moved in verbatim from root -- see README.md's
Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `language_alias.go`, cycling. Reach
  root-only helpers through `codequery` (repository selector, language-query
  grant), `codequery/relationships/story` (the entity-search dispatch its own
  cross-check test exercises), `entitysemantics` (semantic-summary
  attachment), `querycontract` (profiles, envelopes, ports, content model),
  or `querygraphrows` (the shared semantic-metadata Cypher fragment); if none
  of those has what you need, it does not belong here -- ask before adding a
  new shared home.
- `languageQueryCapability` MUST stay `"symbol_graph.language_entities"` --
  byte-identical -- it is the route's registered capability id in
  `specs/capability-matrix.v1.yaml` and a documented OpenAPI error contract.
- The four Cypher builders' shipped text (via `BuildCypherWithSemanticFilter`)
  stays exactly what it was pre-move: `cypher_shipped_text_test.go` freezes
  it byte-for-byte, moved verbatim from root. Do not reflow, reindent, or
  reorder clauses without re-measuring on the pinned NornicDB build and
  updating that baseline plus the queryplan `source_sha256` pin together.
- `languageHandlerTracer` is this package's own tracer var (the same seam
  `incidentHandlerTracer` in `go/internal/query/incident/handler.go` uses):
  package-local so `span_test.go`'s recording-provider swap stays private to
  this package. Do not promote it to an exported var or move the span helper
  back to root.
- `EntitySearch` is exported at its declaration because root's
  `content_reader_entity_search.go` (Part B, not moved) names it through the
  `languageEntitySearch` alias in `language_alias.go`. `languageEntityContentSearcher`
  has no such caller and stays unexported.
- `SourceBackendForTruthBasis` is exported at its declaration because root's
  `language_query_source_backend_test.go` (kept in root, not moved: it also
  calls root-only `OpenAPISpec()`, which this leaf must never reach back for)
  is the caller that needs it, through the `sourceBackendForTruthBasis`
  forwarder in `language_alias.go`.
- `BuildCypherWithSemanticFilter` is exported at its declaration because the
  live-tagged NornicDB grant proofs in root
  (`language_query_grant_nornicdb_live_test.go` and its siblings, gated
  behind build tags the default test run does not compile) import this
  package and call it directly; root keeps no forwarder for it because the
  default build would flag one as unused.
- `SupportedLanguages` and `SupportedEntityTypes` are exported (unchanged from
  before the move) because root's `entity_metadata_flux_test.go` and the
  OpenAPI spec assembly call them through the forwarders of the same name in
  `language_alias.go`.
- `NormalizedVariants` (registry.go) is exported because root's
  `content_reader_entity_names.go`, `content_reader_entity_search.go`,
  `content_reader_structural_inventory.go`, and
  `content_reader_symbol_search.go` (all Part B, not moved) call it through
  the `normalizedLanguageVariants` forwarder in `language_alias.go`.
- The `var _ querycontract.LanguageEntityContentSearcher = (*ContentReader)(nil)`
  compile-time pin lives in root's `language_alias.go`, not here: `ContentReader`
  is a later lane's family (Part B) and this leaf must never name it.

## Test fixtures hoisted to querytestutil (#6608 rule)

A fixture this package's tests share with root's staying tests moved to
`querytestutil` as an exported helper (one definition; root's own file keeps
calling the identical unqualified name through a thin forward), rather than
being duplicated. Do not re-duplicate any of these back into a `_test.go` copy
here or in root:

- `querytestutil.SearchString` -- root's `neo4j_test.go` keeps a forwarding
  `searchString`.
- `querytestutil.CodeGrantGrantedRepo` / `CodeGrantOtherRepo` -- root's
  `code_grant_test_fixtures_test.go` keeps forwarding consts (93+ existing
  root callers compile unchanged).
- `querytestutil.BoundCanonicalLanguage` -- root's
  `typescript_function_graph_first_test.go` and
  `typescript_graph_metadata_test.go` call it directly (no root forward
  needed; only two call sites).
- `querytestutil.LanguageMetadataSharedPath` / `LanguageMetadataSharedName` /
  `LanguageMetadataSharedStart` -- root's
  `language_query_metadata_repository_key_test.go` keeps forwarding consts.
- `querytestutil.LanguageGrantGrantedEntity` / `LanguageGrantUngrantedEntity`
  -- root's `auth_scoped_language_query_grant_test.go` keeps forwarding
  consts.
- `querytestutil.LanguageQueryGrantEntities` -- root's
  `languageQueryGrantContentStore` (shipped_text_test.go) and
  `languageQueryPlainContentStore` (grant_test.go) call it directly; this
  package's `entity_search_dispatch_test.go` doubles
  (`entitySearchDispatchGrantBoundStore`, `entitySearchDispatchPlainStore`)
  call it too, so all three test the identical fixture data.
- `querytestutil.MockLanguageQueryGraphReader` -- root's
  `language_query_metadata_test.go` keeps a forwarding
  `mockLanguageQueryGraphReader` (its ~17 existing callers compile
  unchanged); this package's `typescript_declaration_family_test.go` and
  `span_test.go` construct the querytestutil type directly.

One exception moved rather than hoisted: `unscopedLanguageQueryGrant`
(`typescript_declaration_family_test.go`) could not go to `querytestutil`
because its return type names `codequery.LanguageQueryGrant`, a handler-family
type `querytestutil`'s own doc.go forbids importing. It had exactly one
caller (this file), so it moved rather than being duplicated.

## Naming

`docs/internal/naming.md` is law: no `language_query_` file prefixes, no
`language/language_query.go`, exported identifiers lose the family stutter
except where the #6608/#6642 forwarder rule above names a specific root
caller. The root `language_alias.go` keeps every old exported spelling for
staying callers.
