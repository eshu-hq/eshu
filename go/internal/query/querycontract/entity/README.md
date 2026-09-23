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
`codequery/relationships/story`, `entity`, `impact` and `queryselector`.
`codequery` and `queryselector` import it as `entitycontract` where a local
variable is named `entity`.

Outbound: the parent `querycontract` for `EntityContent`, `ContentStore` and
`RepositoryAccessFilterFromContext`. The parent must not import this package.

## Telemetry

None. The reader that implements `EntityNameSearcher` owns the spans.
