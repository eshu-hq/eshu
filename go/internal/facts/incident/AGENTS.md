# AGENTS.md - internal/facts/incident guidance

## Read first

1. README.md - package boundary and registry role
2. doc.go - godoc contract for the incident package
3. context.go - incident source kinds and family declaration
4. routing.go - routing evidence kinds and family declaration

## Invariants this package enforces

- Dependency direction stays one way: the parent `internal/facts` package
  and downstream consumers (collectors, projectors, loaders, registry
  commands) may import this package, but this package must not import
  `internal/facts`. Its only `internal/facts/*` dependency is
  `internal/facts/contract`.
- Kind string values are frozen contracts shared with collectors, the
  schema registry, and loaders. Do not rename, re-derive, or reformat them
  in a refactor; a kind rename is a contract change with a migration.
- New versioned families register in the parent `schema_version.go` table;
  an unregistered family is invisible to the classifier and drift guard.
- `contract.Declare` is the identity function the static generator reads.
  Keep declarations as plain package-level vars so the generator and the
  fact-kind-registry verifier keep seeing them.
