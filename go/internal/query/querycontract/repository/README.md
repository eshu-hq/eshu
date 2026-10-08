# querycontract/repository

## What is here

| File | Contents |
| --- | --- |
| `read_models.go` | Repository entry-point, deployment-evidence, relationship-evidence, and service-story target-support read models, stores, and loaders. |
| `summary_read_models.go` | Repository summary, counts, and relationship read models, stores, and loaders, plus `RepositoryRef`, `RepoRef`, and the workload identity entry. |
| `projection.go` | `RepoProjection` (standard Cypher RETURN-list fragment for repository nodes) and `RepoRefFromRow` (graph row to `RepoRef`). |

Every read model answers through the `ContentStore` port; the loaders take
the store as a parameter so doubles stay in the caller. `Available` false
on a summary means the read model holds nothing for the repository:
callers must fall back to the graph counts rather than read a zero-value
summary as authoritative.

Both this leaf and the repository handler family are package `repository`,
which Go allows: files here spell the parent `querycontract.ContentStore`,
and callers that import both spell this leaf `repositorycontract`.

## Dependencies

Inbound: 51 files spell this leaf's qualifier (25 in the repository handler
family, 10 in query root, 5 in service, 3 each in testutil/content, entity,
and deadcode, 2 in readmodel), plus the four query-root aliases
(`RepositoryRef`, `RepoRef`, `RepositoryReadModelSummary`,
`RepositoryRelationshipReadModel`) that keep the pre-move `query.X`
spellings compiling. Outbound: `querycontract` only (`ContentStore`,
`StringVal`, `BoolVal`, `MapSliceValue`). The parent never imports this
leaf.

## Telemetry

None new. The loaders emit no spans, metrics, or logs of their own; the
read path that calls them owns the span, and `RepoProjection` is pure
string formatting.

## Failure modes

Row decoding through the shaping helpers never errors: a missing key or
nil yields the zero value, and a mistyped value renders with `%v`. A
loader whose store cannot answer returns nil, and the caller treats that
as missing data, not as an authoritative empty result.
