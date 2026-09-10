# quality

Code-quality inspection for the code family (`codequery`).

## What lives here

- `request.go` — the inspection request (decodes the route JSON
  unchanged), normalization, supported checks, and bounds.
- `inspect.go` — the bounded refactoring-candidate scan (`Inspect`)
  and its Cypher builder. The caller's grant lands in the
  MATCH-attached `WHERE` before the `SKIP`/`LIMIT`.
- `results.go` — result shaping, trim, thresholds, and next calls.

## What stays in `codequery`

`codequery/inspection.go` holds the HTTP handler and the
capability alias (named by contract tests). The handler resolves the
canonical-scope grant and delegates the read to this leaf.

## Invariants

- The scan reader carries a queryplan source pin with its
  `label_inventory` class (see `query-source-coverage.yaml`): keep
  the label, the bound, and the result limit on any rewrite.
- Cypher shape changes need backend-differential proof (NornicDB vs
  Neo4j row-set equivalence), not just unit tests.
- This package never imports `codequery` or root `query`.
