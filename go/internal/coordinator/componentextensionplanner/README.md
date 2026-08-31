# Component extension planner

## Purpose

`componentextensionplanner` turns one validated, claim-capable component
extension collector instance into a deterministic workflow run and one work
item.

## Ownership boundary

The package owns request validation, claim identity derivation (an optional
host claim when present, the component identity otherwise), requested-scope
privacy, and deterministic run/generation/work-item IDs. It performs no
network, credential, or database work and never resolves component
artifacts.

The root `internal/coordinator` package keeps service scheduling order,
hosted extension egress-policy filtering and audit, durable open-target
admission, retries, queue and lease behavior, and telemetry.
`internal/component` owns the component registry, manifest loading, and
activation host-claim metadata that root reads before this package ever
sees a collector instance's `Configuration` string. Parsing and validating
that `Configuration` string is not this package's contract either: it
belongs to the dependency-neutral `internal/coordinator/componentactivation`
package, because root's `component_activation_config.go` (construction),
`pagerduty_service.go` (PagerDuty exclusion), and `governance_audit.go`
(audit identity) all need that same parsing and none of them is a
component-extension concern. This package is one of `componentactivation`'s
consumers, not its owner.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan
  key.
- `WorkPlanner` implements the root coordinator's `ComponentExtensionPlanner`
  interface.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/componentactivation` supplies `Config`,
  `RuntimeConfig`, and `ParseConfig` — the shared activation-configuration
  contract this package plans from. This package never imports
  `internal/coordinator` or any other coordinator child package.
- `internal/coordinator/plannercontract` validates safe plan keys.
- `internal/component` supplies `ActivationHostClaimMetadata`, used to
  build the (privacy-scoped) requested-scope host payload.
- `internal/facts` builds stable generation/work-item identity.
- `internal/workflow` supplies collector instance validation and durable run
  and work-item contracts.

## Telemetry

This pure planner emits no telemetry. The root coordinator records
`eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`. Workflow rows and
claim status expose durable progress.

No-Observability-Change: this package move adds no metric, span, log field,
status value, queue, worker, lease, retry, or runtime setting. The same root
signals continue to cover scheduling and egress-audit failures.

## Gotchas / invariants

- Claim identity prefers the host claim's source system and scope ID;
  without one, it falls back to the component ID and a derived
  `component:<id>` scope.
- `RequestedScopeSet` never includes raw host configuration paths or
  credentials — only component identity, manifest digest, config handle, the
  normalized host claim, and runtime binding.
- `GenerationID` and `SourceRunID` are minted from the same identity because
  the claimed-collection runtime invariant for non-terraform kinds requires
  them equal (see `collector.validateClaimedGeneration`).
- Every validation error this package returns for a malformed activation
  configuration originates in `componentactivation.ParseConfig`; do not
  duplicate that validation here.

No-Regression Evidence: direct child tests call the production planner and
pin activation-scoped work-item construction, requested-scope privacy,
deterministic generation/source-run/fairness identity across repeated
planning, UTC timestamp normalization, and that an unsupported
`runtime.sdk_protocol` surfaces as a `PlanComponentExtensionWork` error.
Root tests pin the schedulability predicate, egress-policy filtering, and
config-loader activation loading.

No-Concurrency-Change: the planner remains a pure per-call value transform
with no shared state, goroutine, lock, claim, lease, transaction, or retry.
The root retains the Postgres admission transaction and hosted extension
egress-policy decision.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/componentactivation/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
- `docs/public/observability/telemetry-coverage.md`
