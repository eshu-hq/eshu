# AGENTS — internal/reducer/aws

This file guides LLM assistants working in `go/internal/reducer/aws`. Read
it before touching any file in this directory.

## Read first

1. `go/internal/reducer/README.md` — full reducer context, domain catalog,
   and phase coordination model.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — the Phase 1/2/3/4 contract
   that any AWS collector → reducer pipeline must honor.

## Invariants (cite file:line)

- **Scaffold only** — `doc.go:1–5` is explicit: no live projection logic
  lives here. Do not add materialization code to this package; create a new
  package or extend the parent reducer domain catalog.
- **`Validate` enforces non-blank fields** — `contract.go:52–76`; it does
  not check that listed component names map to any implementation.
- **One checkpoint — Phase 1 only** — `contract.go:27–36`; the scaffold
  declares `canonical_nodes_committed` for `cloud_resource_uid`. The
  post-Phase-3 reopen for any domain consuming `resolved_relationships`
  derived from these nodes is not owned by this package.
- **Defensive copies from factory functions** — `DefaultRuntimeContract`
  (`contract.go:41`) and `RuntimeContractTemplate` (`contract.go:48`) both
  call `slices.Clone`; do not take pointers to the internal default and
  mutate it.

## Common changes

### Add a new component to the scaffold

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

- **Scaffold diverging from ADR**: if the scaffold is used as an ADR fixture
  and `Validate` passes on an outdated contract, downstream wiring will
  silently miss required checkpoints. Treat failing `Validate` in tests as a
  hard stop.

## Anti-patterns

- Do not add live projection code to this package. The package is a
  planning artifact. Materialization code belongs in a separate handler
  registered with `internal/reducer.NewDefaultRegistry`.
- Do not export new types that reference concrete graph backend types
  (Neo4j, NornicDB). The scaffold should remain backend-agnostic.

## What NOT to change without an ADR

- The accepted checkpoint (`cloud_resource_uid` at
  `canonical_nodes_committed`). Changing the keyspace or phase alters the
  Phase 1 readiness contract used by DSL and downstream edge domains.
- The component list. Component names are referenced in ADR fixture
  assertions; changing them requires a coordinated ADR update.
