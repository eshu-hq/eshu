# AGENTS - internal/reducer/aws

Read this before touching `go/internal/reducer/aws`.

## Read first

1. `go/internal/reducer/README.md` — full reducer context, domain catalog,
   and phase coordination model.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — the Phase 1/2/3/4 contract
   that any AWS collector → reducer pipeline must honor.

## Mandatory Invariants

- This package is a contract package only. Do not add materialization code
  here; reducer runtime logic belongs in a registered reducer handler.
- `Validate` enforces non-blank fields. It does not prove the named components
  are implemented.
- The accepted checkpoint is Phase 1 only:
  `cloud_resource_uid` at `canonical_nodes_committed`. Any domain consuming
  `resolved_relationships` derived from those nodes still needs the standard
  post-Phase-3 reopen outside this package.
- `DefaultRuntimeContract` and `RuntimeContractTemplate` return defensive
  copies. Do not mutate package globals through returned values.

## Common changes

### Add a new component

1. Append to the `Components` slice in `defaultRuntimeContract` in
   `contract.go`.
2. Update the README component list in the same PR.
3. Add a `contract_test.go` assertion verifying the new component appears in
   `DefaultRuntimeContract().Components`.

### Add a new checkpoint

1. Add a `PublishedCheckpoint` entry to `defaultRuntimeContract.Checkpoints`.
2. If the checkpoint is for a phase that depends on `resolved_relationships`
   (anything beyond Phase 1), document the post-Phase-3 reopen requirement
   in this README explicitly.

## Failure modes

- **Contract drift**: if fixtures accept an outdated contract, downstream
  wiring can miss required checkpoints. Treat failing `Validate` in tests as a
  hard stop.

## Anti-patterns

- Do not add live projection code to this package. Materialization code belongs
  in a separate handler registered with `internal/reducer.NewDefaultRegistry`.
- Do not export new types that reference concrete graph backend types
  (Neo4j, NornicDB). The scaffold should remain backend-agnostic.

## What MUST NOT change without architecture-owner approval

- The accepted checkpoint (`cloud_resource_uid` at
  `canonical_nodes_committed`). Changing the keyspace or phase alters the
  Phase 1 readiness contract used by DSL and downstream edge domains.
- The component list. Component names are referenced in contract fixture
  assertions; changing them requires coordinated tests and package docs.
