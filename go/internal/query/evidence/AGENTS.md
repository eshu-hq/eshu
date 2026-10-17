# evidence — agent instructions

## Read first

`doc.go` for the alias contract, `README.md` for ownership and move evidence.

## Invariants

- `EvidenceCitationHandle` stays a type alias (`=`), never a redefinition:
  every field stays exported and unchanged.
- This package must not import the root query package.
- Sibling importers spell the alias through this package
  (`evidence.EvidenceCitationHandle`); do not re-add a root-level
  `query.EvidenceCitationHandle` forwarding alias.
- The citation endpoint, synthesis helpers, read model, bundle composer,
  and `EvidenceHandler` stay in root query until the full evidence leaf
  move; do not move them here piecemeal — methods on root receiver types
  cannot live in another package.

## Common changes

Repointing another caller at the alias: swap only the
`query.EvidenceCitationHandle` qualifier, keep the `query` import when the
file uses other root symbols, and rerun that package's tests.
