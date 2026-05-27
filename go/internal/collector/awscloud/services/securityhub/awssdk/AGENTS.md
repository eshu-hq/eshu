# AGENTS.md - internal/collector/awscloud/services/securityhub/awssdk guidance

## Read First

1. `README.md` - package purpose, API allowlist, telemetry, and invariants.
2. `client.go` - Security Hub SDK client construction, snapshot orchestration,
   and telemetry.
3. `list.go` - hub-adjacent list and describe calls.
4. `findings.go` - GetFindings aggregate reduction and finding-body boundary.
5. `../scanner.go` - scanner-owned Security Hub fact selection.
6. `../README.md` - Security Hub scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Security Hub SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe Security Hub metadata.
- Reduce GetFindings responses to aggregate counts in memory. Never return or
  log finding IDs, resources, remediation, notes, product fields,
  user-defined fields, network details, or process details.
- Never copy insight filters into scanner-owned types.
- Do not call mutation APIs: BatchUpdateFindings, BatchImportFindings,
  CreateInsight, DeleteInsight, UpdateInsight, EnableSecurityHub,
  DisableSecurityHub, EnableStandards, DisableStandards, CreateActionTarget,
  DeleteActionTarget, UpdateActionTarget, BatchEnableStandards, or
  BatchDisableStandards.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Security Hub metadata read by extending `Client`, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned
  types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend finding aggregation only with bounded posture dimensions such as
  severity, standard, control, compliance status, or workflow status.

## What Not To Change Without An ADR

- Do not persist finding details, insight filters, automation rule bodies, or
  Security Hub mutation results.
- Do not infer workload, environment, deployment, account hierarchy, or
  ownership truth from hub names, standards, controls, members, tags, or
  insights.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
