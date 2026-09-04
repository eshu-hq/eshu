# AGENTS.md — internal/coordinator/awsfreshnessplanner guidance

## Read first

1. `README.md` for ownership and invariants.
2. `planner.go` for target-scope parsing, authorization, trigger coalescing,
   and deterministic workflow-row construction.
3. `../service_aws_freshness.go` for the `AWSFreshnessPlanner` interface, the
   trigger claim/lease/reap loop, and root scheduling and admission.
4. `../aws_scheduled_scheduler.go` — a DIFFERENT AWS family that shares this
   package's `target_scopes` parsing. It is not extracted; do not move its
   symbols here and do not conflate the two.
5. `../plannercontract/README.md` for plan-key grammar.

## Invariants

- Keep AWS API calls and credential resolution out. This package decides which
  configured targets are authorized; it never contacts AWS.
- Keep all methods on `coordinator.Service` in the parent package.
- Do not import the parent coordinator package.
- Preserve exact validation errors, run/work-item/generation IDs, and the
  per-account `FairnessKey` (`aws:<instance_id>:<account_id>`).
- Preserve requested-scope metadata privacy: only `collector_instance_id` and,
  per target, `account_id`, `region`, `service_kind`, `scope_id` — never
  credentials or the raw configuration document.
- `ParseTargetScopes` and `TargetAuthorized` are exported on purpose. Root's
  `findAWSFreshnessInstance` filter and this planner's rejection are two halves
  of one authorization decision; do not fork a second copy at root to avoid the
  import. This is the `gcpplanner` precedent (`EnabledScopes`,
  `ValidateClaimSchedulerConfiguration`), not the `ociregistry` `firstNonBlank`
  one — that precedent is for genuinely tiny pure helpers, and this block is
  ~80 lines of decoding plus the authorization predicate.
- Do not read `scheduled_scan_enabled` here. It shares the configuration
  document but belongs to the scheduled AWS family at root.
- Do not name this package `awsfreshness`: the repo already aliases
  `internal/collector/awscloud/freshness` as `awsfreshness` in coordinator and
  Postgres tests, and the collision is what forced the `-planner` suffix.

## Common changes

Write a failing planner test before changing target-scope parsing,
authorization, trigger coalescing, or requested-scope construction. Claim
leases, reaping, scheduling order, the plan-key clock, durable admission,
retries, and telemetry changes belong in the parent
(`service_aws_freshness.go`).

Adding a new AWS service kind: register it under
`internal/collector/awscloud/services/<service>/runtimebind` and in the
`awsruntime/bindings` aggregator. Nothing here needs to change —
`normalizeList` asks `awsruntime.SupportsServiceKind`.

## Failure modes

Invalid instances, a non-AWS collector kind, a disabled or non-claim-capable
instance, an empty trigger batch, a zero observation time, an unsafe plan key,
malformed configuration JSON, a missing or empty `target_scopes`, a
non-12-digit `account_id`, an empty or `*` region or service entry, an
unsupported `service_kind`, and any unauthorized target all fail before a
workflow row is returned. Unauthorized targets fail the batch rather than being
dropped, so a root/child disagreement is loud instead of silent.

Tests here need the AWS scanner registry populated. `aws_bindings_test.go`
blank-imports `awsruntime/bindings` for that; deleting it makes every valid
`service_kind` unsupported and the failures will look like planner bugs.

## Verification

Run `go test ./internal/coordinator/awsfreshnessplanner ./internal/coordinator -count=1`
first, then the recursive coordinator suite, package-doc, and dirgate checks,
and whole-module build and vet.
