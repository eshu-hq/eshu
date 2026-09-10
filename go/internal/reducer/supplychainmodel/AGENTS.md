# Agent instructions: internal/reducer/supplychainmodel

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The 15 DTO shapes the supply-chain-impact family's classifier reads to decide
one CVE-x-package finding — advisory, affected-package/range, consumption,
SBOM, OS-package, scanner attachment, deployment/workload/service-context, and
risk-signal rows — plus `ScopeGenerationKey`, the pure join-key helper
`OSPackage` and `ScannerAnalysis` share.

It exists so a future split of the supply_chain_impact + suppression family
(67 files, over the repo's 40-file dirgate cap) can read this shared
vocabulary without importing the reducer root. The root's
`SupplyChainImpactHandler.Handle()` passes these types into six clusters; a
cluster subpackage that needed the shapes from the root would create an
import cycle the moment the root also needed that cluster. This package
breaks that cycle in advance.

## Hard rules

**Never import `internal/reducer`**, directly or transitively. If you want a
type from the root here, either move that type here too (and update this
package's doc comments and every reducer-root call site that names it to
match), or reconsider whether the code belongs in this package at all.

**Keep it plain data and one pure function.** No method, no `context.Context`,
no I/O, no queue/graph handle. The only dependency is `internal/facts`, used
by `ScopeGenerationKey` alone.

**Do not move `supplyChainImpactIndex` here.** It aggregates these DTOs
alongside four reachability-cluster types (`GoVulnerabilityFinding`,
`jsTSPackageReachabilityIndex`, `pythonReachabilityRepositoryEvidence`,
`jvmReachabilityIndex`) and one image-identity type
(`supplyChainImageIdentity`) that this package does not own. Moving it either
drags those five types in (out of this PR's scope) or forces this package to
import the reducer root back for them — the exact cycle this package exists
to avoid. It stays at the reducer root.

**Do not move the 7 orchestration functions that stay at the root**
(`classifySupplyChainImpactPackage` and its helpers in
`index.go`). This package is the type hoist only; the
family's cluster split is a later PR. The root originally had 8 such
functions; the 8th, `supplyChainScopeGenerationKey`, was a pure
key-derivation helper rather than orchestration logic, and it already moved
here as `ScopeGenerationKey`.

## Changing an exported type

Adding a field is usually safe. Removing or renaming a type or field is not
without checking both directions:

- Every reducer-root reference to a type here spells the qualified
  `supplychainmodel.*` name directly — there is no compat alias file to
  update, but renaming a *type* still requires a repo-wide rename of every
  `supplychainmodel.<OldName>` call site under `internal/reducer/*.go` and
  `internal/reducer/*_test.go`.
- Every field access across the reducer root and its tests uses the exported
  field name directly (there is no alias for fields, since Go does not support
  aliasing a struct field) — renaming a *field* requires a repo-wide check of
  `internal/reducer/*.go` and `internal/reducer/*_test.go` for that field name,
  scoped to the specific type, not a blind text search (several of these DTOs
  share field names like `FactID`, `RepositoryID`, `PackageID` with unrelated
  types that stay at the root).

## `ScopeGenerationKey`

Must stay a thin forward to `facts.Envelope{...}.ScopeGenerationKey()`, never
a reimplementation. `OSPackage` and `ScannerAnalysis` evidence is correlated by
this exact key; drifting from the platform's own scope-generation format
breaks that join silently rather than failing loudly.
