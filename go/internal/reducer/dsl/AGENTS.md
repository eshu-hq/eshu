# AGENTS - internal/reducer/dsl

Read this before touching `go/internal/reducer/dsl`.

## Read first

1. `go/internal/reducer/README.md` — full reducer context, phase
   coordination model, and the post-Phase-3 reopen requirement.
2. `go/internal/reducer/AGENTS.md` — invariants governing all reducer
   sub-packages.
3. `CLAUDE.md` "Facts-First Bootstrap Ordering" — Phase 1–4 ordering.
   Specifically: any domain that consumes `resolved_relationships` must have
   a post-Phase-3 reopen; `OutputKindResolvedRelationship` publications from
   this package feed those rows.

## Mandatory Invariants

- This package owns the seam, not DSL evaluation logic. Concrete substrates
  land outside this package.
- `OutputKindResolvedRelationship` feeds `resolved_relationships`; consumers
  need a post-Phase-3 reopen outside this package.
- `PhaseStates` deduplicates and sorts by `(AcceptanceUnitID, Keyspace,
  Phase)`. Callers must not depend on insertion order.
- `cross_source_anchor_ready` is reserved for DSL substrate code. Do not
  publish it from canonical projectors or unrelated reducer handlers.
- `PublishEvaluationResult` is nil-safe. Verify the publisher is non-nil in
  integration tests so production wiring mistakes do not hide behind no-ops.
- `DefaultRuntimeContract` and `RuntimeContractTemplate` return defensive
  copies.

## Common changes

### Add a new `OutputKind`

1. Add the constant to `evaluator.go` alongside `OutputKindResolvedRelationship`.
2. If the new output kind feeds `resolved_relationships`, document the
   post-Phase-3 reopen obligation in this README.
3. Add a `contract_test.go` or `evaluator_test.go` case.

### Add a new checkpoint to the DSL scaffold

1. Append to `defaultRuntimeContract.Checkpoints` in `contract.go`.
2. If the new phase gates a domain that is currently blocked, verify the
   `sharedProjectionReadinessPhase` switch in
   `internal/reducer/shared_projection.go:91` is updated accordingly.
3. Update this README's checkpoint table.

### Implement a concrete `Evaluator`

- The evaluator belongs in a separate package, not here. It must satisfy
  the `Evaluator` interface (`evaluator.go:41`) and return an
  `EvaluationResult` whose `Publications` use only keyspaces and phases
  declared in `internal/reducer/graph_projection_phase.go`.

## Failure modes

- **Missing `cross_source_anchor_ready` row**: downstream edge domains that
  wait for this phase will block in the shared projection runner and log
  "skipped intents until semantic readiness is committed". Check whether the
  DSL evaluator ran and whether `PublishEvaluationResult` was called with a
  non-nil publisher.
- **Duplicate `resolved_relationships` rows**: if the evaluator runs multiple
  times for the same `(AcceptanceUnitID, Keyspace, Phase)` tuple,
  `PhaseStates` deduplicates within one result but separate calls to
  `PublishEvaluationResult` will each write a row. Ensure idempotency at
  the caller.

## Anti-patterns

- Do not add evaluation logic to this package. The package owns the seam.
- Do not publish `cross_source_anchor_ready` from outside DSL substrate code.
- Do not skip `PhaseStates.Validate` return check; a blank `AcceptanceUnitID`
  will silently produce a broken row.

## What MUST NOT change without architecture-owner approval

- The `OutputKind` constants. They are referenced in contract tests and
  downstream domain expectations.
- The five accepted checkpoints in `defaultRuntimeContract`. Changing them
  alters the cross-source readiness contract used by deployment mapping and
  workload materialization.
- The deduplication logic in `PhaseStates`; it is a correctness property,
  not an optimization.
