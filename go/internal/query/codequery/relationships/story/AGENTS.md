# AGENTS.md — codequery/relationships/story

Bonded leaf of the code family, nested under the `relationships`
leaf. The story reads are load-bearing on backend behavior and grant
semantics; the cypher-query-rigor skill governs any change to them.

## Ownership

- `data.go` owns the payload assembly and its shapers. The
  floor-before-truncation ordering is classifier load-bearing.
- `resolution.go` owns target resolution and the grant-bound reads.
  `SearchEntitiesForGrant` is twin-pinned against the language-query
  read; change both sides together.
- `class.go` owns hierarchy/override builders and shapers. Depths
  come from filtered rows; truncation flags from raw backend counts.
- `graph.go` owns the direct builders and the grant predicates.
  Predicates sit on entity nodes, never on optional aliases.
- `nornicdb.go` owns the NornicDB builders and projection
  normalization.
- `codequery/story_handlers.go`, `codequery/story_reads.go`,
  `codequery/story_nornicdb.go`, and `codequery/story_forwarders.go`
  own the handlers, the pinned readers, the hot cypher builders, and
  the forwarders. Do not move the pinned bodies here.

## Rules for agents

- May import the parent `relationships` leaf; never import
  `codequery` or root `query` from this package.
- Never touch the eight queryplan-pinned story bodies or the hot
  cypher text in `codequery` (digest freeze); re-freezing is not a
  fix.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
- Grant-predicate changes need the fail-closed proof: unattributable
  rows drop, cross-repo still constrains both ends.
