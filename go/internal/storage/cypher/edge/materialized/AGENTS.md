# AGENTS.md — Cypher materialized-edge registry guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Cypher storage conventions.
3. `materialized_edge_families.go` for the family registries,
   `materialized_edge_endpoints.go` for endpoints, and
   `materialized_edge_repo_dependency.go` for the alternation and split
   retract builder.

## Invariants

- Registry reason strings must cite the exact writing template or
  reaping constant; `TestRegistryReasonsCiteRealSymbols` fails the
  package on invented citations.
- Never add a type to a registry without the writer change that emits
  it; registries describe coverage, they do not create it.
- Keep the repo-dependency alternation derived from the live retract
  statements, never relisted by hand.
- Keep the package clause as `package materialized`; external callers
  use the `materialized` alias.
- Never import this package from the parent `cypher` package.
