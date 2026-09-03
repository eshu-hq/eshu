# Terraform-state scheduler planner

## Purpose

`tfstateplanner` plans one workflow work item per resolved Terraform-state discovery
candidate — seeded backends, Terraform backend blocks, and Terragrunt
`remote_state` blocks — without opening the state source or reading a state
payload.

## Ownership boundary

This package owns `PlanRequest`, plan-request and plan-key validation,
discovery-config parsing, candidate resolution through its injected ports,
requested-scope construction, and deterministic workflow-row construction. The
parent coordinator retains the `TerraformStatePlanner` interface, scheduling
order, the plan-key clock, durable open-target admission, the
waiting-on-git-generation log-and-continue path, retries, and telemetry.
Methods on `coordinator.Service` remain in the parent package
(`tfstate_service.go`).

Following the `ociregistry` extraction (#6491), the `TerraformStatePlanner`
interface itself stays in `service.go` rather than moving into
`tfstate_service.go`: issue #6057 scopes this change to the `_scheduler.go`
half only and treats decomposing `Service`'s interface block as a separate
design decision. `service.go` imports this package solely to name
`PlanRequest` in that interface's method signature; the child's `WorkPlanner`
satisfies it structurally, with no explicit declaration needed.

Unlike its sibling planners, `WorkPlanner` is not pure. It carries two
injected ports and calls them during planning. That is why
`go/cmd/workflow-coordinator/main.go` constructs it with fields rather than as
an empty struct.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan
  key.
- `WorkPlanner` implements the parent's `TerraformStatePlanner` interface via
  `PlanTerraformStateWork`. Its `GitReadiness` and `BackendFacts` fields are
  the discovery ports the caller wires.

See `doc.go` for the godoc contract.

## Dependencies

`internal/collector/terraformstate` supplies `DiscoveryResolver`,
`ParseDiscoveryConfig`, `CandidatePlanningID`, and the two port interfaces.
`scope` and `workflow` provide stable identities and durable row contracts.
This package does not import its parent, and it holds no shared helper with
root: every private function that moved here has no other caller in the
repository. Unlike the `ociregistry` extraction, there was no `firstNonBlank`-
style helper to duplicate.

It also does not import `plannercontract`. Terraform-state keeps its own
stricter plan-key validator, for the reason in the invariants below.

## Telemetry

None. Planning inherits the parent reconcile metrics, workflow and claim
status, and the parent's waiting-on-git-generation log line.

No-Observability-Change: this package move adds no metric, span, log field,
status field, queue, worker, lease, retry, or runtime setting. Coordinator
failures remain visible through
`eshu_dp_workflow_coordinator_reconcile_total`,
`eshu_dp_workflow_coordinator_reconcile_duration_seconds`, workflow row and
claim status, and the existing admission logs.

## Gotchas / invariants

- **The plan-key validator is deliberately stricter than
  `plannercontract.ValidateSafePlanKey`.** It rejects any key containing
  `s3://`, `/`, or `\` before the character allowlist runs, because a
  Terraform-state plan key sits inside the run ID and a locator-shaped key
  would publish bucket and object path into a durable row. Do not "simplify"
  this into the shared validator.
- **No raw locator material reaches a durable field.** Work-item and
  generation identity is `terraformstate.CandidatePlanningID`, a hash;
  `RequestedScopeSet` carries only `scope_id`, `candidate_id`, `source`, and
  `backend_kind`, sorted by `candidate_id`.
- **Zero resolved candidates is an empty plan, not an error.** The planner
  returns a zero-value run with no items and a nil error, and the parent skips
  creating workflow rows. This differs from `ociregistry`, where an empty
  target set is a validation failure — here the configuration is validated
  separately from what discovery resolves, so a valid graph or backend-filter
  configuration that matches nothing is a legitimate no-op rather than a
  misconfiguration. The moved tests do not exercise that path; they cover the
  seeded configurations, which always resolve at least one candidate.
- **`FairnessKey` is per collector instance**
  (`terraform_state:<instance_id>`), not per candidate — coarser than the
  per-`scope_id` fairness the observability planners use. Changing it
  repartitions claim fairness, so it is a concurrency change, not a cosmetic
  one.
- **`SourceRunID` and `GenerationID` are both the candidate planning ID.**
  `AcceptanceUnitID` is the candidate's `RepoID`, falling back to the scope
  partition key when the candidate carries no repo.
- **Terragrunt is resolved before the planner sees it.** `BackendFacts`
  returns Terragrunt `remote_state` candidates already resolved into their
  underlying s3 or local backend kind, so this planner never observes
  `BackendTerragrunt` and needs no second scheduler shape.
- **The waiting-on-git-generation path lives at root.** This planner returns
  the resolver's error unchanged; `tfstate_service.go` classifies it with
  `terraformstate.IsWaitingOnGitGeneration` and continues.

No-Regression Evidence: `go test ./internal/coordinator/tfstateplanner ./internal/coordinator -count=1`
proves seed-candidate planning with pinned run identity and locator-free work
item IDs, runtime compatibility of the planned item with
`tfstateruntime.ClaimedSource`, distinct recurring run and work-item identity
per plan key, rejection of an invalid durable discovery config, and
requested-scope hashing that names the scope and candidate IDs while excluding
the bucket, key, and version ID — plus the root scheduling and admission
wiring through `fakeTerraformStatePlanner`. This is a same-behavior file move:
no lease, conflict-key, retry, batching, or ordering change.

## Related docs

- `go/internal/coordinator/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
