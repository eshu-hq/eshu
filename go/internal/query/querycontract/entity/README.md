# querycontract/entity

Entity-name search contract and exact graph entity resolution, shared by the
code, entity and impact query families.

## What is here

| file | holds |
| --- | --- |
| `name_search.go` | `EntityNameSearch`, `EntityNameSearcher`, the match and scope enums, limits and sentinel errors |
| `repo_identity.go` | `ClearResolvedEntityRepoProjectionPlaceholders`, which drops graph-projected repo placeholders before repo identity is hydrated |
| `resolution.go` | `ResolveExactGraphEntityCandidate(s)`, `SelectExactGraphEntityCandidate`, `ExactEntityNameMatches`, `NonTestEntityMatches`, `IsTestEntityPath`, `FormatAmbiguousEntityMatches` |

## Dependencies

Inbound: 20 files across `internal/query`, `codequery`, `codequery/search`,
`codequery/relationships/story`, `entity`, `impact` and `selector`.
`codequery` and `selector` import it as `entitycontract` where a local
variable is named `entity`. The `query/entity` handler package imports it
under the plain name `entity` too: both are package `entity`, which Go allows.

Outbound: the parent `querycontract` for `EntityContent`, `ContentStore` and
`RepositoryAccessFilterFromContext`. The parent must not import this package.

## Telemetry

None. The reader that implements `EntityNameSearcher` owns the spans.
