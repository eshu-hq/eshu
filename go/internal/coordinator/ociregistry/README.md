# OCI registry scheduler planner

## Purpose

`ociregistry` plans one workflow work item per configured OCI registry
repository target — Docker Hub, GHCR, ECR, Google Artifact Registry, Azure
Container Registry, JFrog, and Harbor — without opening a registry connection
or resolving credentials.

## Ownership boundary

This package owns `PlanRequest`, configured-target parsing and field
normalization, per-provider repository identity resolution, duplicate-target
rejection, and deterministic workflow-row construction. The parent coordinator
retains the `OCIRegistryPlanner` interface, scheduling order, the plan-key
clock, durable open-target admission, retries, and telemetry. Methods on
`coordinator.Service` remain in the parent package (`oci_registry_service.go`).

Unlike sibling extractions (`gcpplanner`, `prometheusmimir`, `vaultlive`), the
`OCIRegistryPlanner` interface itself stays in `service.go` rather than moving
into `oci_registry_service.go`: issue #6057 scopes this PR to the
`_scheduler.go` half only and treats decomposing `Service`'s interface block
as a separate design decision. `service.go` imports this package solely to
name `PlanRequest` in that interface's method signature; the child's
`WorkPlanner` satisfies it structurally, with no explicit declaration needed.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan
  key.
- `WorkPlanner` implements the parent's `OCIRegistryPlanner` interface via
  `PlanOCIRegistryWork`.

See `doc.go` for the godoc contract.

## Dependencies

`internal/collector/ociregistry` and its per-provider adapters (`acr`,
`dockerhub`, `ecr`, `gar`, `ghcr`, `harbor`, `jfrog`) resolve each configured
target into the shared normalized repository identity. `plannercontract`
validates plan keys. `facts`, `scope`, and `workflow` provide stable
identities and durable row contracts. This package does not import its parent
and performs no I/O.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and duplicate-admission logs.

No-Observability-Change: this package move adds no metric, span, log field,
status field, queue, worker, lease, retry, or runtime setting. Coordinator
failures remain visible through `eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow row and
claim status, and the existing admission logs.

## Gotchas / invariants

- Run IDs are a function of instance, resolved trigger kind (schedule or
  bootstrap), and plan key. Work-item and generation IDs are a function of
  instance, plan key, and the target's normalized scope ID.
- A blank configuration, one that decodes to `{}`, and one with an empty
  `targets` array are all validation failures, not empty plans. Two independent
  guards reject them before the planner examines a target count:
  `validatePlanRequest` (via the instance's own configuration validation) and
  `parseOCIRegistryRuntimeTargets`, both surfacing
  `OCI registry collector configuration requires targets` from
  `internal/workflow/oci_registry_config.go`. The planner's zero-target early
  return is therefore unreachable through this path — see
  `TestOCIRegistryWorkPlannerRejectsBlankConfiguration`, which fails if either
  guard stops rejecting.
- Two configured targets that normalize to the same repository identity
  (different registry spelling, casing, or provider-specific defaulting) are
  rejected as a duplicate before any work item is built — see
  `TestOCIRegistryWorkPlannerRejectsDuplicateNormalizedTargets`.
- ECR, Google Artifact Registry, and Azure Container Registry each accept a
  configured `registry_host` (or, for ECR, `registry`) ahead of any
  provider-computed default; a package-local `firstNonBlank` helper picks the
  first non-blank value. This is a deliberate duplicate of the parent's own
  `firstNonBlank` — the parent's copy still serves its package-registry and
  vulnerability-intelligence planners, which are not extracted here, so do not
  try to unify the two into one shared export.
- Requested-scope metadata (`RequestedScopeSet`) sorts targets by `scope_id`
  and carries only `scope_id`, `provider`, and `repository` — never
  credentials, base URLs, or tag limits.

No-Regression Evidence: `go test ./internal/coordinator/ociregistry ./internal/coordinator -count=1`
proves request validation, configured-target parsing and normalization,
per-provider identity resolution across all seven providers including the
GHCR default-host and lowercase paths, duplicate normalized-target rejection
(asserted on the specific error text and colliding scope ID, not merely a
non-nil error), blank-configuration rejection, run, work-item, generation and
fairness identities pinned byte-for-byte by
`TestOCIRegistryWorkPlannerPinsExactIdentityStrings`, and the root
scheduling/admission wiring through `fakeOCIRegistryPlanner`. This is a same-behavior file move: no lease,
conflict-key, retry, batching, or ordering change.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
