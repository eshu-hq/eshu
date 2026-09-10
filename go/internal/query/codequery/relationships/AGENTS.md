# AGENTS.md — codequery/relationships

Bonded leaf of the code family. The reads are load-bearing on
backend behavior; the cypher-query-rigor skill governs any change to
them.

## Ownership

- `nornicdb.go` owns the row readers and the dialect patterns. The
  row ceiling and truncation flags are structural (see README).
- `enrich.go` owns enrichment. Partial File-without-Repository
  metadata must keep flowing.
- `identity.go` owns label resolution and the inheritance grant
  filter. The story family consumes the inheritance filter and the
  node pattern from here.
- `filters.go` owns shaping and capabilities.
- `codequery/relationship_handlers.go`,
  `codequery/relationship_forwarders.go`,
  `codequery/transitive_walk.go`, and
  `codequery/entity_labels.go` own the handler,
  the content path, the pinned methods, and the forwarders. Do not
  move the methods here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- Never touch the four grandfathered method bodies in `codequery`
  (digest freeze); re-freezing is not a fix.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
