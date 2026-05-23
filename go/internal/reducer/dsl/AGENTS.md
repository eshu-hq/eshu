# internal/reducer/dsl Agent Rules

These rules are mandatory for changes under `go/internal/reducer/dsl`.

## Read first

1. `README.md` and `doc.go`.
2. `go/internal/reducer/README.md` and `go/internal/reducer/AGENTS.md`.
3. `docs/internal/agent-guide.md` section "Bootstrap And Correlation Truth"
   before changing publications that feed `resolved_relationships`.

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

## Change Rules

- New `OutputKind`: add the constant, document any
  `resolved_relationships` reopen obligation, and add contract coverage.
- New checkpoint: update the runtime contract, readiness-phase routing, README
  checkpoint table, and tests together.
- Concrete evaluator: implement it outside this package. It MUST satisfy
  `Evaluator` and publish only declared reducer keyspaces and phases.

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

## Anti-Patterns

- Do not add evaluation logic to this package. The package owns the seam.
- Do not publish `cross_source_anchor_ready` from outside DSL substrate code.
- Do not skip `PhaseStates.Validate` return check; a blank `AcceptanceUnitID`
  will silently produce a broken row.

## Forbidden Without Architecture-Owner Approval

- The `OutputKind` constants. They are referenced in contract tests and
  downstream domain expectations.
- The five accepted checkpoints in `defaultRuntimeContract`. Changing them
  alters the cross-source readiness contract used by deployment mapping and
  workload materialization.
- The deduplication logic in `PhaseStates`; it is a correctness property,
  not an optimization.
