# GCP planner

## Purpose

`gcpplanner` turns one validated GCP collector instance into a deterministic
workflow run and one work item per enabled configured Cloud Asset Inventory
scope. It also exposes the same scope parsing and validation the root
coordinator's freshness handoff loop and startup config loader need, so
neither has to depend on this package's private configuration types.

## Ownership boundary

The package owns request validation, live-collection opt-in enforcement,
scope-configuration parsing and field defaulting, scope-ID derivation,
duplicate and field validation, requested-scope filtering, requested-scope
privacy, deterministic IDs, and per-scope fairness metadata. It performs no
network, credential, or database work.

The root `internal/coordinator` package keeps service scheduling order, the
clock and plan-key cadence, tenant-grant authorization, durable open-target
admission, GCP freshness trigger claim/handoff/reap, retries, queue and lease
behavior, and telemetry. `internal/collector/gcpcloud` owns provider calls and
fact emission after a worker claims the planned item.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, plan key,
  and optional scope filter for freshness wake-ups.
- `WorkPlanner` implements the root coordinator's `GCPPlanner` interface.
- `EnabledScopes` parses a collector instance's configuration and returns
  every enabled, validated scope as a privacy-scoped `ConfiguredScope`. Root's
  freshness handoff loop calls this to resolve which configured scopes an
  inbound Cloud Asset Inventory change-event trigger authorizes.
- `ConfiguredScope` is the subset of one scope's fields root's freshness
  matching needs: scope, parent scope, asset-type family, and location
  bucket. It omits content_family (a CAI change event carries no
  content_family signal) and the credential reference.
- `ValidateClaimSchedulerConfiguration` reports whether a GCP collector
  instance's declarative configuration is safe to admit for claim-enabled
  scheduling. Root's config loader calls this for every enabled,
  claim-enabled GCP instance before accepting it into `Config`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/plannercontract` validates safe plan keys.
- `internal/collector/gcpcloud` and `internal/collector/gcpcloud/gcpruntime`
  supply parent-scope-kind validation and scope-ID derivation.
- `internal/facts` builds stable generation IDs.
- `internal/scope` supplies the GCP collector kind.
- `internal/workflow` supplies collector instance validation and durable run
  and work-item contracts.

## Telemetry

This pure planner emits no telemetry. The root coordinator records
`eshu_dp_workflow_coordinator_reconcile_total` and
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`; GCP freshness
handoff additionally records `eshu_dp_gcp_freshness_events_total` and
`eshu_dp_gcp_freshness_fanout_scope_count`. Workflow rows and claim status
expose durable progress.

No-Observability-Change: this package move adds no metric, span, log field,
status value, queue, worker, lease, retry, or runtime setting. The same root
signals continue to cover scheduling and handoff failures.

## Gotchas / invariants

- Live collection must be explicitly enabled (`live_collection_enabled=true`)
  before any scope is planned; an instance without it fails planning even when
  claims are enabled.
- Scopes are sorted by scope ID before planning, so work-item order is
  deterministic regardless of configured JSON array order.
- A scope with a blank `scope_id` gets one derived from its parent scope kind,
  parent scope ID, and (defaulted) asset-type family, content family, and
  location bucket.
- Optional scope fields default: `asset_type_family` to `mixed`,
  `content_family` to `resource`, `location_bucket` to `global`.
- `credential_ref` is required on every scope and is never included in
  `RequestedScopeSet` or `ConfiguredScope`.
- A valid request whose `ScopeIDs` filter matches no configured scope still
  returns a populated pending run and no items; the root skips durable
  admission for that empty item slice.
- `EnabledScopes` and `ValidateClaimSchedulerConfiguration` surface the exact
  same validation errors `PlanGCPWork` returns for the same configuration,
  because both call the same private parse and validate helpers.
- Run, generation, and work-item IDs are deterministic for a fixed request.

No-Regression Evidence: direct child tests call the production planner and
pin request-field validation, live-collection opt-in enforcement, sorted
scope order, disabled-scope skipping, scope-configuration field validation,
default derivation, deterministic IDs and fairness keys, requested-scope
privacy, freshness scope filtering including a valid empty selection, and
that `EnabledScopes`/`ValidateClaimSchedulerConfiguration` surface the same
validation `PlanGCPWork` does. Root tests pin GCP config-loader startup
rejection and active-mode scheduling, skip, and tenant-scope-filtering
behavior. Planning scans `n` configured scopes in O(n) and sorts them in
O(n log n). It opens no network or database connection.

No-Concurrency-Change: the planner remains a pure per-call value transform
with no shared state, goroutine, lock, claim, lease, transaction, or retry.
The root retains the Postgres admission transaction and GCP freshness trigger
claim/handoff/reap state transitions. Per-instance fairness keys retain their
prior conflict domain.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
- `docs/public/observability/telemetry-coverage.md`
