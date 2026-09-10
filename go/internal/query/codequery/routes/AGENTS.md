# AGENTS.md — codequery/routes

Bonded leaf of the code family. Two files here are load-bearing on
backend behavior; the cypher-query-rigor skill governs any change to
them.

## Ownership

- `graph.go` owns the row join, the directional CALLS traversal, and
  the label gate. Grant binding (request selectors + caller grant
  before `LIMIT`) is structural: keep both conjuncts on any rewrite.
- `entry.go` owns validation and route selection. The grant check at
  the read is separate; the scope bound is the request's own scope,
  not the grant.
- `impact.go` owns caller/callee shaping and the access predicates.
- `codequery/route_handlers.go` owns the handler, the impact assembler, the
  grandfathered methods, the aliases, and the forwarders. Do not move
  the methods here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- Never touch the four grandfathered method bodies in
  `codequery/route_handlers.go` (digest freeze); re-freezing is not a fix.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
