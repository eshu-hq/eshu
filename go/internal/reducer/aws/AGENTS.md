# internal/reducer/aws Agent Rules

These rules are mandatory for changes under `go/internal/reducer/aws`.

## Read first

1. `README.md` and `doc.go`.
2. `go/internal/reducer/README.md` and `go/internal/reducer/AGENTS.md`.
3. `docs/internal/agent-guide.md` section "Bootstrap And Correlation Truth"
   before changing readiness checkpoints.

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

## Change Rules

- New component: update the runtime contract, README component list, and
  contract assertions together.
- New checkpoint: update the runtime contract, README checkpoint table, and
  tests. If it depends on `resolved_relationships`, document the post-Phase-3
  reopen requirement here.

## Failure modes

- **Contract drift**: if fixtures accept an outdated contract, downstream
  wiring can miss required checkpoints. Treat failing `Validate` in tests as a
  hard stop.

## Anti-Patterns

- Do not add live projection code to this package. Materialization code belongs
  in a separate handler registered with `internal/reducer.NewDefaultRegistry`.
- Do not export new types that reference concrete graph backend types
  (Neo4j, NornicDB). The contract must remain backend-agnostic.

## Forbidden Without Architecture-Owner Approval

- The accepted checkpoint (`cloud_resource_uid` at
  `canonical_nodes_committed`). Changing the keyspace or phase alters the
  Phase 1 readiness contract used by DSL and downstream edge domains.
- The component list. Component names are referenced in contract fixture
  assertions; changing them requires coordinated tests and package docs.
