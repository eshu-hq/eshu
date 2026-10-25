# Security-alert scheduler

## Purpose

`alert` plans the workflow work items of configured provider security alert
targets. It validates target configuration and builds deterministic run,
generation, and work-item identities without opening a provider connection.

A repository target is one work item. An org target is one work item per shard
of its partition (`internal/scope/org/shard`, #9019): the operator's
`scope_id` when the target holds at most 100 distinct same-org repositories,
otherwise `<scope_id>#shard-<bits>` scopes of at most 100 each.

The planner also retires what it no longer plans. Through `ShardScopeReader`
(`internal/storage/postgres/security/alerts/retirement` in production) it reads
the scopes of each org target, the base scope and every shard whose parent is
the base, that still hold an active generation that is not already a
retirement. Each one outside the current shard set gets one retirement work
item: same scope, `acceptance_unit_id = <scope_id>#retire`, a generation id
hashed with `retire`. The collector answers it with an empty generation
committed with `TrustedDeletion`, the freshness hint
`shard.RetirementFreshnessHint` (the retirement marker), and a read state with
`repositories_total = 0`, which the reader skips. The reader skips any
collection with 0 too; it holds no alert fact, so nothing stays readable. A split retires the leaf's old name (or the base)
and a merge retires both leaves; the merged parent is collected again.

## Ownership boundary

This package owns the security-alert planning request and planner
implementation. The parent `internal/coordinator` package keeps the planner
interface, `Service` scheduling position, clock-derived plan key, tenant and
egress filtering, durable open-target admission, retry behavior, and telemetry.
The `security_alert_service.go` half stays in the parent package because its
methods use the shared `Service` type.

## Exported surface

- `PlanRequest` carries the collector instance, observation time, and plan key.
- `WorkPlanner` implements the parent package's `SecurityAlertPlanner`
  interface. Its `ShardScopes` field is required for an org target; `Logger`
  is optional.
- `ShardScopeReader` is the store port for retirement candidates.
- `MaxRetirementCandidates` (1024) bounds retirements per instance per scan.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/coordinator/planner/contract` validates the shared plan-key grammar.
- `internal/facts` builds stable generation identities.
- `internal/scope` and `internal/workflow` provide collector and durable
  workflow contracts.
- `internal/scope/org/shard` provides the org-target partition, the shard id
  format, and the retirement marker.

The package does not import the parent coordinator package.

## Telemetry

Planning runs inline during the parent coordinator's reconcile pass and
inherits its reconcile metrics, workflow rows, claim status, duplicate-skip
logs, and `/api/v0/index-status` surface. Two logs are its own (#9019), with
counts only, never a scope id or repository name:

- Info `security alert planner retiring unplanned org target scopes` with
  `collector_instance_id`, `org_target_count`, `planned_shard_scope_count`,
  and `retired_scope_count`, once per scan that retires anything.
- Warn `security alert planner reached the per-scan retirement bound` with
  `retirement_bound`, when one scan hits `MaxRetirementCandidates`; the rest
  retire on later scans.

The retirement read runs through the coordinator's `InstrumentedDB` with
store name `security_alert_shard_scopes`, so its duration and errors show on
the Postgres store metrics and spans. A failed read fails the instance's plan. `scheduleSecurityAlertWork` returns
that first error as `plan security alert work for <instance>`, and the first
scheduler error ends the coordinator's whole reconcile pass for that cycle
(the `reconcile_error` outcome), the same as any other scheduled planner's
error; the next pass retries.

## Gotchas / invariants

- Tenant grants (parent `workflow_tenant_grants.go`): a shard item, and the
  retirement of a shard or base scope, is authorized by the base scope's
  grant, since operators grant the configured target and never a generated
  shard id (validation refuses a configured `scope_id` with `#shard-`). An
  ungranted or stale-policy base denies its shards and retirements; other
  collector kinds keep exact-id lookup.

- The planner never resolves `token_env` or copies it into
  `requested_scope_set`.
- Run and work-item identities depend on the collector instance, plan key, and
  target scope. Replaying the same request returns the same identities.
- Duplicate target scope IDs are rejected before any workflow row is returned.
- A collection item keeps `acceptance_unit_id == scope_id` and its prior
  generation identity, so a target that does not shard plans exactly what it
  planned before #9019.
- The shard set is a pure function of the configuration: replaying a
  reconciliation yields the same shard ids, retirement ids, and generation ids.
- Retirement covers merged-away shards and the base of a split target. A
  target removed from the configuration, or re-scoped (switched to a
  repository target, or its `scope_id` renamed), is not retired here (#9071).
- The plan key is validated but not normalized.

No-Regression Evidence: the focused child test calls the production planner.
The full parent and binary package tests compile the root interface, service
call, and concrete wiring after the import move. Whole-module build and vet
verify the same `alert.WorkPlanner` wiring through the root
`SecurityAlertPlanner` interface.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/planner/contract/README.md`
- `docs/internal/design/package-restructure.md`
- `docs/public/reference/source-layout.md`
