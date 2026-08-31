# AGENTS.md - querycontract

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary and proof requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Invariants

- Keep this package dependency-neutral. It may use the Go standard library but
  must not import the root `query` package, graph drivers, or Postgres adapters.
- Preserve response JSON field names and envelope negotiation.
- Preserve unknown-capability panic text and profile ceiling semantics.
- Register family capabilities through `RegisterCapabilities`; duplicate
  initialization attempts are contract failures.
- Preserve the selector presence tri-state on `K8sSelectCandidate`.
- `RepositoryAccessFilter`'s fields (`AllScopes`, `AllowedScopeIDs`,
  `AllowedRepositoryIDs`, `Allowed`) stay exported: root and family test files
  build the struct with keyed literals directly rather than through a
  constructor. Do not re-introduce unexported fields without updating every
  call site.
- `ScopeGrantInlineParamPrefix` and the `scope_grant_<i>` param naming
  convention it defines are shared by `ScopeGrantInlineMapDisjunction` (the
  predicate builder) and `BindScopeGrantInlineScalars` (the param binder).
  Keep both in this package so the two stay coupled to one constant; a
  duplicated copy of the prefix in another package can silently drift and
  produce a predicate that references params nobody binds.

## Verification

Run focused `querycontract` and root `query` tests, then whole-module build and
vet. Run `scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a shared wire or port type only when multiple query families need it
  without importing the root package.
- Move a capability registration with its family and preserve the canonical
  YAML order through the root ordering gate.
- Keep root aliases and function wrappers until every external caller has a
  separately reviewed migration path.

## Failure modes

- A copied type instead of an alias breaks source identity across storage and
  handler adapters.
- Dropping selector-presence fields collapses absent and present-empty values.
- Root-only capability registration makes a moved family panic when tested by
  itself.
- Reordering capability initialization can change the canonical inventory.

## Anti-patterns

- Do not add handler orchestration, Cypher, SQL, or family-specific response
  models here.
- Do not expose graph or Postgres implementations through the neutral ports.
- Do not replace root function wrappers with mutable function variables.

## ADR-controlled changes

Changing capability overwrite semantics, removing the root compatibility
layer, or moving route assembly into this package requires an accepted
architecture decision before implementation.
