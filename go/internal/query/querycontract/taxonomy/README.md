# querycontract/taxonomy

The naming vocabulary the query handler families share: language
normalization, the user-facing entity-type maps, graph-row metadata
projection, and the language-scoped entity search shapes.

It was planned as `querycontract/language`. Only three of its seventeen exported
symbols are about languages, a handler package named `language` already exists
at `query/language`, and a package named `language` would need a rename or
import alias in six of its 28 importing files, five of them because a
`language` local shadows the import. `taxonomy` has none of those problems.

## What is here

| file | holds |
| --- | --- |
| `language.go` | `CanonicalLanguage`, `NormalizedLanguageVariants`, `CoverageLanguageMaps` over an unexported alias table |
| `entity_types.go` | `GraphBackedEntityTypes`, `ContentBackedEntityTypes`, `ResolveContentBackedEntityTypes`, `ContentEntityTypeForResolve`, `GraphFirstContentBackedEntityTypes`, `ElixirSemanticEntityType(s)`, `ElixirGraphSemanticEntityType`, `GraphLabelToContentEntityType` |
| `result_metadata.go` | `GraphResultMetadata` |
| `search.go` | `LanguageEntitySearch`, `LanguageEntityContentSearcher`, `LanguageResultMatchKey`, `ResultContentEntityType` |

The two grantless no-read constants that used to sit beside these
(`NoBackendReadSourceBackend`, `ReasonEmptyGrantNoBackendRead`) did not move:
they are truth-basis wire spellings and live in the parent's `truth.go` next to
`TruthBasisNoBackendRead`.

## Dependencies

Inbound: 22 non-test files and 6 test files import this package, all under
`go/internal/query`.

Outbound: the parent `querycontract` (`EntityContent`,
`RepositoryLanguageCount`) and `querycontract/rowvalue`. The parent must not
import this package, or the import becomes a cycle.

## Telemetry

None. The package holds maps and pure functions; the handlers that call it own
their spans and metrics.
