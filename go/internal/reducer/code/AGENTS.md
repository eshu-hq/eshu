# code — agent instructions (issue #6061)

This directory is a namespace, not a package with behavior. Do not add
runtime code to `doc.go`; add it to the child package that owns it.

## Invariants

- Imports point strictly downward: the reducer root imports the children;
  the children never import the reducer root or each other's unexported
  surface. From the reducer tree, `call/` and `taint/` import only the shared
  tier (`contract`, `factload`, `factdecode`, `schemadecode`, `sharedintent`,
  `payloadcore`); `value/` imports `payloadcore` and the exported surface of
  its sibling `taint/`; `shell/` imports the shared tier plus the sibling
  leaves `sqlrelationship` (its delta-scope builder) and `call/`
  (`PayloadInt`); `function/summary/` imports the shared tier plus the
  sibling leaf `value/` (for `FixpointProjectionResult`); `semantic/` imports
  the shared tier (`contract`, `factload`, `gpphase`, `payloadcore`).
  Everything else they import is outside `internal/reducer`.
- A new child here is a named destination in
  `docs/internal/design/reducer-target-tree.md`, never a new top-level
  reducer sibling. Amend the tree doc in the same PR.
- Moves into this tree are behavior-preserving: prove them with the
  recursive reducer suite, B-7 golden corpus, and B-12 replay coverage, all
  byte-identical, before claiming done.
- Root files named `code_*.go` collide with this directory under the dirgate
  naming rule. Each stayer carries its own justified `//nolint:dirgate`
  marker; the naming-exempt ledger only shrinks and takes no new rows.
