# supply — agent instructions (issue #6776)

This directory is a path namespace, not a package with behavior. Do not add
runtime code to `doc.go`; add it to `chain/` (or a future sibling), the
child package that owns it.

## Invariants

- A new child here is a named destination for a glued compound that
  `docs/internal/naming.md` rule 3 requires to split. Never a new glued
  `facts` sibling such as `supplychain`.
- Identifiers inside `chain/` must not re-acquire a `SupplyChain*` stutter
  (naming.md rule 4).
- A facts-root file named `supply.go` or `supply_*.go` collides with this
  directory under dirgate's sibling-subpackage naming rule
  (`scripts/lib/dirgate-core.sh`, `dirgate_naming_violation_subpkg`). The
  root's compatibility surface for this family is `compat_supply_chain.go`
  for exactly that reason; keep new root additions off the `supply_` prefix.
- Moves into or within this tree are behavior-preserving: prove them with
  the affected package's tests, `go vet ./...` across the module, and a
  `go test -list '.*'` diff against the base.
