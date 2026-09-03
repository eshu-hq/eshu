# AGENTS.md — internal/coordinator/ociregistry guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for target parsing, per-provider identity resolution, and
   deterministic workflow-row construction.
3. `../oci_registry_service.go` for root scheduling and durable admission.
4. `../service.go` for the `OCIRegistryPlanner` interface — unlike sibling
   extractions, it stays there rather than moving into `oci_registry_service.go`
   (issue #6057: Service decomposition is a separate design decision).
5. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep provider calls and credential resolution out. This package resolves a
  configured target into a normalized repository identity; it never opens a
  registry connection.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve exact validation errors, run/work-item/generation IDs, and the
  per-target `FairnessKey` (`<collector_kind>:<instance_id>:<provider>`).
- Preserve requested-scope metadata privacy: only `scope_id`, `provider`, and
  `repository`, sorted by `scope_id` — never credentials, base URLs, region,
  registry IDs, or tag limits.
- Reject two configured targets that normalize to the same repository
  identity before any work item is built.
- Keep the package-local `firstNonBlank` helper local. The parent package
  keeps its own copy for package-registry and vulnerability-intelligence
  planners that are not extracted here; do not merge the two into a shared
  export for a five-line pure function.

## Common changes

Write a failing planner test before changing target parsing, per-provider
identity resolution, duplicate detection, or requested-scope construction.
Scheduling order, the plan-key clock, tenant/egress filtering, durable
admission, retries, and telemetry changes belong in the parent
(`oci_registry_service.go`).

Adding a new OCI registry provider: add the adapter under
`internal/collector/ociregistry/<provider>`, add a `case` in
`ociRegistryTargetIdentity`, and add coverage in `planner_test.go` mirroring
`TestOCIRegistryWorkPlannerNormalizesProviderEndpointFields`.

## Failure modes

Invalid instances, zero observation times, unsafe plan keys, malformed
configuration JSON, an unsupported provider, and duplicate normalized targets
all fail before any workflow row is returned. A blank or empty-target
configuration is one of those failures, not an empty plan. Both
`validatePlanRequest` and `parseOCIRegistryRuntimeTargets` reject it
independently, each surfacing
`OCI registry collector configuration requires targets`. Do not "restore" an
empty-plan path for it — the zero-target early return in `PlanOCIRegistryWork`
is unreachable through this path, and a change that made it reachable would
silently turn a misconfiguration into a successful no-op.

## Verification

Run `go test ./internal/coordinator/ociregistry ./internal/coordinator -count=1`
first, then the recursive coordinator suite, scoped race, package-doc, and
dirgate checks, and whole-module build and vet.
