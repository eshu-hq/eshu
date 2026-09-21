# AGENTS.md - internal/coordinator/semantic guidance

## Read first

1. `go/internal/coordinator/semantic/README.md` for this worker's ownership
   boundary and invariants.
2. `go/internal/coordinator/semantic/provider_worker.go` for the
   claim/egress/dispatch loop.
3. `go/internal/coordinator/semantic/provider_config.go` for
   environment-driven configuration and validation.
4. `go/internal/coordinator/semantic/provider_metrics.go` for the claim
   counter and its label dimensions.
5. `go/internal/coordinator/service_maintenance.go` for where the parent
   coordinator invokes `Run`.
6. `go/internal/semanticpolicy/README.md` for the egress policy contract.

## Invariants

- Never call `Dispatch` before the egress gate has allowed the claim.
- Keep the default `DisabledProviderClient` a true no-network implementation;
  a real provider client must ship in a separate, security-reviewed PR gated
  behind `ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED`.
- Keep `GovernanceAuditAppender` declared in this package; do not import
  `internal/coordinator` to reuse its audit interface instead.
- Keep the metric prefix an argument to `NewProviderWorkerMetrics`; do not
  hardcode it or import `coordinator.MetricPrefix` here.
- Keep `ProviderClaimObservation` fields redacted and low-cardinality; never
  add a host, endpoint, URL, credential, prompt, or response field.
- Do not rename the `ESHU_SEMANTIC_PROVIDER_*` environment variable strings;
  only the exported Go identifiers were de-stuttered on the move.

## Common changes

- New egress outcomes or failure classes need a new bounded `ProviderOutcome*`
  constant, a corresponding case in `handleClaim`, and a focused worker test.
- New claim metric dimensions require a failing telemetry test first and a
  cardinality review against `telemetry-coverage-discipline`.
- Config changes need a failing `LoadProviderWorkerConfig` test and
  `validate()` coverage before the change.

## Failure modes

- A claim denied by egress policy is skipped through `SkipClaimByPolicy` and
  recorded as `egress_denied` or `egress_policy_missing`.
- An egress-allowed claim with no enabled client is dead-lettered as
  `provider_disabled` through `DeadLetterClaim`.
- A `Dispatch` error is dead-lettered under
  `semanticqueue.FailureClassProviderUnavailable`.

## Verification

Run focused child tests, then the recursive coordinator tree. Build and vet
the whole module because `cmd/workflow-coordinator` owns the concrete worker
wiring and the `DisabledProviderClient` default.
