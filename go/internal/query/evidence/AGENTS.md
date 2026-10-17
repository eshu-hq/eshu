# evidence — agent instructions

## Read first

`doc.go` for the family contract, `README.md` for ownership and move evidence.

## Invariants

- `EvidenceCitationHandle` stays a type alias (`=`), never a redefinition:
  every field stays exported and unchanged.
- This package must not import the root query package.
- The deployable-unit packet route composes only through
  `DeployableUnitPacketResponder`, which root implements from the lane-B
  packet envelope; the handler passes only leaf values and the live request,
  and the scope gate never crosses the seam. When the
  investigation/packet leaf lands, delete the seam and call the leaf
  directly.
- Capability rows are declared once, in `capabilities.go`; root's contract
  matrix calls those constructors, and `main_test.go` registers them for
  this package's test binary. Never copy a row.
- The queryplan-pinned `repositoryCountForEvidenceBundle` keeps its
  byte-identical source: the `GraphQuery`/`IntVal` aliases above it exist
  for that pin. Any Cypher change re-pins `source-coverage/first.yaml`.
- The status helpers at the bottom of `bundle_live.go` are hoisted copies
  of root's status plumbing; root keeps its originals. Keep both copies
  identical.
- Sibling importers spell the alias through this package
  (`evidence.EvidenceCitationHandle`); do not re-add a root-level
  `query.EvidenceCitationHandle` forwarding alias.

## Common changes

Repointing another caller at the alias: swap only the
`query.EvidenceCitationHandle` qualifier, keep the `query` import when the
file uses other root symbols, and rerun that package's tests.
