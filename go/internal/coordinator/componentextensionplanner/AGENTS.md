# Component extension planner agent guide

## Read first

1. `planner.go` — request validation and workflow-row construction.
2. `planner_contract_test.go` — deterministic IDs/fairness keys across
   repeated planning and UTC timestamp normalization.
3. `planner_validation_test.go` — activation-scoped work-item construction
   and the pass-through rejection of an invalid activation configuration.
4. `../componentactivation/config.go` and `AGENTS.md` — the shared
   activation-configuration contract this package plans from; it is NOT
   owned here.
5. `../component_extension_service.go` and `../service_component_extension_test.go`
   — root scheduling, egress-policy filtering, and the
   `ComponentExtensionPlanner` interface this package implements.
6. `../plannercontract/README.md` — shared safe plan-key rules.

## Ownership

This package owns pure component-extension planning: request validation,
claim identity derivation, requested-scope privacy, and deterministic
workflow-row construction from an already-parsed
`componentactivation.Config`. It does not parse or validate raw activation
configuration (that is `componentactivation.ParseConfig`'s contract), read
the component registry, read Postgres, admit or claim work, evaluate hosted
extension egress policy, retry requests, or emit telemetry.

The root coordinator owns scheduling order, hosted extension egress-policy
filtering and audit, durable open-target admission, retries, queue and lease
behavior, and reconcile telemetry. Keep those responsibilities in the parent
package.

**Never import `internal/coordinator` or reach for
`componentInstanceConfig`-shaped root state.** This package's own
`PlanRequest`/`WorkPlanner` types satisfy the root `ComponentExtensionPlanner`
interface structurally — root imports this package for those types, not the
other way around. If a change here seems to need a root symbol, it almost
certainly means the symbol belongs in `componentactivation` (if unrelated
root callers also need it) or should stay a root responsibility passed in
through `PlanRequest` (if it is scheduling context, not activation-config
shape). This is exactly the tangle #6057's component-extension lane hit: the
activation configuration type could not move into this package alone,
because `pagerduty_service.go` and `governance_audit.go` — unrelated
providers — also depend on it. It was hoisted into the dependency-neutral
`componentactivation` package instead, the same shape `projector/intent`
uses for the projector families' equivalent problem.

## Invariants

- Prefer the host claim's source system and scope ID for claim identity;
  fall back to the component ID and a derived `component:<id>` scope.
- Keep raw host configuration paths and credentials out of
  `RequestedScopeSet`.
- Mint `GenerationID` and `SourceRunID` from the same identity value.
- Keep all timestamps in UTC and all IDs deterministic for a fixed request.
- Do not duplicate `componentactivation.ParseConfig`'s validation logic
  here; call it and propagate its error.

## Verification

Run focused child and parent tests first, then recursive coordinator tests and
the scoped race suite. At minimum, run
`go test ./internal/coordinator/componentactivation ./internal/coordinator/componentextensionplanner ./internal/coordinator -count=1`.
Mutation checks must break the production assertion and must be reverted
before broader verification.
