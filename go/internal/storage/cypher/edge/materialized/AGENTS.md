# AGENTS.md — Cypher materialized-edge registry guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Cypher storage conventions.
3. `families.go` for the shared-projection family registry,
   `single_type_families.go` for the single-type DIRECT half (the
   file is past the 500-line cap, so a new single-type family registers
   from its own file with an `init()` that adds one entry, as
   `runtime_declaration_family.go` does),
   `direct_families.go` for the multi-type DIRECT
   table (`RetractKind`, `RetractVariants`),
   `family_identity.go` for the identity and
   enumeration accessors over those tables,
   `endpoints.go` for endpoints, and
   `repo_dependency.go` for the alternation and split
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
