# GCP planner agent guide

## Read first

1. `planner.go` — request validation, scope-configuration parsing and
   defaulting, scope-ID derivation, filtering, and workflow-row construction.
2. `planner_contract_test.go` — ordering, determinism, privacy, fairness
   partitions, and the `EnabledScopes`/`ValidateClaimSchedulerConfiguration`
   exports.
3. `planner_edge_test.go` — invalid requests, scope-configuration validation
   branches, and default derivation.
4. `planner_validation_test.go` — the original planner-level coverage this
   package inherited from the pre-extraction `gcp_scheduler_test.go`.
5. `../gcp_service.go`, `../service_gcp_test.go`, `../service_gcp_freshness.go`,
   and `../config.go` — root scheduling, freshness handoff, and the
   claim-scheduler config-loader gate this package's exports serve.
6. `../plannercontract/README.md` — shared safe plan-key rules.

## Ownership

This package owns pure GCP Cloud Asset Inventory planning and configured-scope
parsing/validation. It validates the four-field `PlanRequest`, parses and
defaults every configured scope, derives scope IDs when absent, plans scopes
in sorted order, and builds deterministic workflow rows. It does not resolve
credentials, call Google Cloud, read Postgres, admit or claim work, change
freshness trigger state, retry requests, or emit telemetry.

The root coordinator owns scheduling order, clock-derived plan keys,
tenant-grant authorization, durable open-target admission, GCP freshness
trigger claim/handoff/reap, retries, queue and lease behavior, and reconcile
telemetry. Keep those responsibilities in the parent package.

Two root call sites depend on this package's parsing beyond `PlanGCPWork`:
`service_gcp_freshness.go`'s `resolveGCPFreshnessScopeIDs` (matching an
inbound Cloud Asset Inventory change-event trigger against configured scopes)
and `config.go`'s `validateCollectorClaimSchedulingSupported` (startup
validation of a claim-enabled GCP instance). Both call the exported
`EnabledScopes`/`ValidateClaimSchedulerConfiguration` wrappers rather than
reaching into this package's private types — the same shape
`jiraplanner.HasConfiguredScope` and `pagerdutyplanner.HasConfiguredScope` use
for their own root freshness call sites. If root needs another read of
configured-scope data, add another narrow exported wrapper here rather than
exporting the private configuration types directly.

## Invariants

- Require `live_collection_enabled=true` before admitting any scope; a
  claim-enabled instance without it fails planning.
- Plan scopes in scope-ID sorted order regardless of configured JSON order.
- Default `asset_type_family` to `mixed`, `content_family` to `resource`, and
  `location_bucket` to `global`; derive `scope_id` from the (defaulted)
  parent-scope and family fields when it is blank.
- Reject a scope missing `credential_ref`, an invalid `parent_scope_kind`, a
  blank or delimiter-containing `parent_scope_id`, or an invalid
  `asset_type_family`/`content_family`/`location_bucket`.
- Reject duplicate `scope_id` values within one configuration.
- Keep `credential_ref` and `content_family` out of `RequestedScopeSet` and
  out of `ConfiguredScope`.
- A valid empty scope selection returns a populated pending run and no items.
- Keep all timestamps in UTC and all IDs deterministic for a fixed request.
- `EnabledScopes` and `ValidateClaimSchedulerConfiguration` must return the
  same validation error `PlanGCPWork` would for the same configuration; do not
  let them drift into separate validation logic.

## Verification

Run focused child and parent tests first, then recursive coordinator tests and
the scoped race suite. At minimum, run
`go test ./internal/coordinator/gcpplanner ./internal/coordinator -count=1`.
Mutation checks must break the production assertion and must be reverted
before broader verification.
