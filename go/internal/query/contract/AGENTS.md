# Agent instructions: query/contract

- This package MUST NOT import `internal/query`. It writes the registry root
  reads, via `querycontract.RegisterCapabilities`; importing root would make
  the cycle this package exists to avoid.
- New rows MUST call `register` (registry.go), never write a registry map
  directly. A direct write skips duplicate and ordering bookkeeping, which is
  how a repeated key previously escaped
  `querycontract_boundary_test.go`'s `DuplicateCapabilityRegistrations` check.
- A capability key MUST be either the owning leaf package's exported const, or
  a string literal. Do NOT introduce a cross-package const alias for a key: the
  capability sweep gate resolves literals, not aliases, so an alias trades a
  visible duplication for an unverifiable capability argument. See
  `querycontract/capability.go`.
- When a family moves out of the query root, switch its key here from the
  literal to that leaf's exported const in the same PR, and delete root's copy
  in `capability_keys.go` if no root handler still names it.
- Adding a row REQUIRES the matching entry in `specs/`'s capability YAML.
  `TestCapabilityMatrixMatchesYAMLContract` compares sizes both ways, so a row
  added on one side only fails it.
- This directory sits at the dirgate 40-file cap. A new family file needs one
  of the existing files to merge or the package to split first; do not add a
  grandfather row for it.
