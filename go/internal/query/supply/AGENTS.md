# supply — agent instructions (issue #6642)

This directory is a path namespace, not a package with behavior. Do not add
runtime code to `doc.go`; add it to `chain/` (or a future sibling), the
child package that owns it.

## Invariants

- A new child here is a named destination for a glued compound that
  naming.md rule 3 requires to split (for example a future `supply/openapi/`
  mirroring `openapi/paths/supply/chain/`, #6648). Never a new glued
  top-level `query` sibling.
- `chain/` (package `chain`) is commonly imported under the alias
  `supplychain` for readability at call sites; that alias never licenses
  renaming identifiers back to a `SupplyChain*` stutter inside `chain`,
  `advisory`, `impact`, or `alerts` (naming.md rule 4).
- Root files named `supply_*.go` collide with this directory under
  dirgate's sibling-word naming rule. Root's compatibility surface for the
  supply-chain family lives in `compat_supply_chain.go` (not `supply_*.go`)
  for exactly this reason; keep new root compat additions off the
  `supply_` prefix too.
- Moves into or within this tree are behavior-preserving: prove them with
  the affected package's tests, the whole-module build/vet, and the
  `go test -list` diff before claiming done.
