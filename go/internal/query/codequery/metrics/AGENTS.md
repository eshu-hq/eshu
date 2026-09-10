# AGENTS.md — codequery/metrics

Bonded leaf of the code family. The builder text is queryplan-pinned
(`cypher_sha256` for QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE):
any change to the MATCH/RETURN/LIMIT shape must re-validate the
hot-cypher manifest and the plan claim in the same PR.

## Ownership

- `edges.go` owns the edge pass, the scan-limit bound, and the param
  bindings (`edge_scan_limit`, `repo_id`). No grant predicate belongs
  here — see `doc.go`.
- `codequery/graph_metrics.go` owns the HTTP handler, the grant-bound
  data method, and the const alias. Do not move the methods here;
  methods must live in their type's package.

## Rules for agents

- Never import `codequery` or root `query` from this package.
- Never redeclare the scan limit; alias the exported const.
- New files need a useful Go doc comment; no placeholders.
- Bump nothing without proof: the grant short-circuit and the
  overflow sentinel are covered by route-level tests in `codequery`.
