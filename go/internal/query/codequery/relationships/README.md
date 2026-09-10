# relationships

Entity relationship reads for the code family (`codequery`).

## What lives here

- `nornicdb.go` — the NornicDB row readers (`GraphRow`,
  `OneHopRelationships`, `MetadataRow`, `TransitiveRows`), the
  dialect pattern builders, the row limits, and placeholder
  normalization.
- `enrich.go` — file/repository enrichment of the core rows
  (`EnrichRows`).
- `identity.go` — entity-label resolution, the metadata builders,
  and the inheritance grant filter the story family builds on.
- `filters.go` — response shaping, direction/type filtering, and
  the contract capabilities.

## What stays in `codequery`

`codequery/relationship_handlers.go` holds the HTTP handler, the
content-fallback path, the repo-identity hydration, and the two pinned
graph-row readers. `codequery/relationship_forwarders.go` keeps the
pre-move spellings that forward onto this leaf.
`codequery/transitive_walk.go` holds the pinned one-hop read
and the transitive walk that injects it.
`codequery/entity_labels.go` holds the pinned label
read and forwarders. The name-target resolver already lives in
`codemodel`.

## Invariants

- Four graph-read methods are grandfathered: their bodies never
  change without a re-validation the ledger forbids — re-freezing is
  not a fix. New logic goes in this leaf behind the existing pins.
- The row ceiling (`RowLimit`) and its truncation flags are
  exact-truth load-bearing: never present a clipped set without the
  flags.
- Cypher shape changes need backend-differential proof (NornicDB vs
  Neo4j row-set equivalence), not just unit tests.
- This package never imports `codequery` or root `query`.
