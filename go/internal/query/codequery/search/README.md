# search

Entity search and result enrichment for the code family (`codequery`).

## What lives here

- `names.go` — global entity-name search over the content store
  (`SearchGlobalEntityNames`): substring or exact match, grantless
  callers resolve to no rows.
- `enrich.go` — content-metadata enrichment of graph search results
  (`EnrichResultsWithContentMetadata`,
  `EnrichResultsWithContentMetadataByEntityID`) and the graph/content
  metadata merge (`MergeMetadata`).

## What stays in `codequery`

`codequery/global_name_search.go` holds the `searchGlobalEntityNames`
thin method plus five grandfathered-support shims for the relationships
reads (they keep pinned bodies' call sites byte-identical; see that
file). `codequery/result_enrichment.go` holds the enrich thin methods
and the `ResultContentEntityType` wrapper (exported seam). The
dead-code investigation next-call shapers moved to the `deadcode`
leaf, which owns them.

## Invariants

- `MergeMetadata`'s absent shape is nil (not an empty map); the wire
  tells the two apart. Keep the nil contract on any rewrite.
- This package never imports `codequery` or root `query`.
