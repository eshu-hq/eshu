# AGENTS.md — codequery/quality

Bonded leaf of the code family. The scan is load-bearing on backend
behavior; the cypher-query-rigor skill governs any change to it.

## Ownership

- `inspect.go` owns the scan and its builder. Grant binding (grant
  in the MATCH-attached `WHERE` before `SKIP`/`LIMIT`) is
  structural: keep it on any rewrite.
- `request.go` owns validation and bounds.
- `results.go` owns shaping.
- `codequery/inspection.go` owns the handler and the
  capability alias. Do not move the handler here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- The scan's `label_inventory` pin stays bound (label, bound,
  result limit); keep them on any rewrite.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
