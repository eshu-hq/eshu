# AGENTS.md — codequery/imports

Bonded leaf of the code family. The row readers are load-bearing on
backend behavior; the cypher-query-rigor skill governs any change to
the reads.

## Ownership

- `rows.go` owns the investigation reads and module scopes. Grant
  binding (request scope + caller grant before `LIMIT`) is
  structural: keep both conjuncts on any rewrite.
- `params.go` owns the builder parameter map and scope shaping.
- `codequery/import_dependencies_execution.go` owns the alias and
  the forwarders. Do not move the methods here.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- The four QP-CODE-IMPORT entry ids stay bound to their readers;
  keep counts, key bounds, and result limits on any rewrite.
- New files need a useful Go doc comment; no placeholders.
- Cypher changes need backend-differential proof (NornicDB vs Neo4j
  row-set equivalence), not just unit tests.
