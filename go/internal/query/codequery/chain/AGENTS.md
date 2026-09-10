# AGENTS.md — codequery/chain

Bonded leaf of the code family. Two files here are load-bearing on
backend behavior; the cypher-query-rigor skill governs any change to
them.

## Ownership

- `cypher.go` owns both shortestPath dialects, the anchor set, and the
  hop predicates. Grant binding (request scope + caller grant before
  `LIMIT`) is structural: keep both conjuncts on any rewrite.
- `request.go` owns validation and repository resolution. The grant
  check at the read is separate; this bound is the request's own
  scope, not the grant.
- `nodes.go` owns node projection and endpoint candidacy.
- `codequery/callers.go` owns the methods, the alias, response
  shaping, and the frozen-named forwarder. Do not move the methods
  here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- Never touch the two grandfathered method bodies in `callers.go`
  (digest freeze); re-freezing is not a fix.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
