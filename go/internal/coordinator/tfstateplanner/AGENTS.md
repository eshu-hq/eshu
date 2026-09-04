# AGENTS.md — internal/coordinator/tfstateplanner guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for plan-request validation, discovery-config parsing,
   candidate resolution, and deterministic workflow-row construction.
3. `../tfstate_service.go` for root scheduling, the plan-key clock, the
   waiting-on-git-generation continue path, and durable admission.
4. `../service.go` for the `TerraformStatePlanner` interface — like
   `ociregistry`, it stays there rather than moving into `tfstate_service.go`
   (issue #6057: Service decomposition is a separate design decision).
5. `../../collector/terraformstate` for `DiscoveryResolver`,
   `CandidatePlanningID`, and the two port interfaces this planner depends on.

## Invariants

- Keep state reading out. This package resolves discovery candidates into
  workflow rows; it never opens a state source, downloads a state payload, or
  resolves AWS credentials.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve exact validation errors, the run ID format
  (`terraform_state:<instance_id>:<trigger_kind>:<plan_key>`), the work-item
  and generation IDs, and the per-instance `FairnessKey`
  (`terraform_state:<instance_id>`). These strings drive lease ownership and
  claim fairness, so changing one is a concurrency change.
- Keep `validateTerraformStatePlanKey` local and stricter than
  `plannercontract.ValidateSafePlanKey`. It rejects `s3://`, `/`, and `\`
  before the character allowlist because the plan key is embedded in the run
  ID; the shared validator does not carry that locator-leak guard. Do not
  replace it with the shared one.
- Preserve requested-scope metadata privacy: only `scope_id`, `candidate_id`,
  `source`, and `backend_kind`, sorted by `candidate_id` — never bucket, key,
  region, version ID, or role ARN.
- Unlike `ociregistry`, this extraction duplicated no root helper. Every
  private function here has no other caller in the repository, so if you find
  yourself needing one of root's helpers, prefer copying a small pure function
  over exporting it — the same call `ociregistry` made for `firstNonBlank`.

## Common changes

Write a failing planner test before changing plan-request validation, plan-key
validation, candidate-to-work-item mapping, or requested-scope construction.
Scheduling order, the plan-key clock, the waiting-on-git-generation log,
durable admission, retries, and telemetry changes belong in the parent
(`../tfstate_service.go`).

Adding a new discovery source: add it under
`internal/collector/terraformstate` so `DiscoveryResolver` resolves it into a
`DiscoveryCandidate`; this package should need no change, because it plans
from candidates rather than from source kinds. Terragrunt already arrives
resolved into its underlying backend kind for exactly that reason.

## Failure modes

An invalid instance, a non-`terraform_state` collector kind, a disabled or
non-claim-capable instance, a zero observation time, an unsafe plan key,
invalid durable configuration, an unparseable discovery config, and a resolver
error all fail before any workflow row is returned. A resolver error that
`terraformstate.IsWaitingOnGitGeneration` recognizes is not a planner concern:
it is returned unchanged and the parent logs and continues. Zero resolved
candidates is an empty plan with a nil error, not a failure — do not turn it
into one.

## Verification

Run `go test ./internal/coordinator/tfstateplanner ./internal/coordinator -count=1`
first, then the recursive coordinator suite, scoped race, package-doc, and
dirgate checks, and whole-module build and vet.
