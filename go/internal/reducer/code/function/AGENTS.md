# code/function — agent instructions (issue #6061)

This directory is a namespace, not a package with behavior. Do not add
runtime code to `doc.go`; add it to the child package that owns it.

## Invariants

- Imports point strictly downward: the reducer root imports the children;
  the children never import the reducer root. From the reducer tree,
  `summary/` imports the shared tier (`contract`, `factdecode`,
  `schemadecode`, `payloadcore`) plus the sibling leaf `code/value` (for
  `FixpointProjectionResult`); outside `internal/reducer`, it
  imports `internal/parser/summary` (aliased `parsed`, since the package name
  collides with this package's own `summary`) and `internal/parser/interproc`.
- A new child here is a named destination in
  `docs/internal/design/reducer-target-tree.md`, never a new top-level
  reducer sibling. Amend the tree doc in the same PR.
- Moves into this tree are behavior-preserving: prove them with the
  recursive reducer suite before claiming done.
