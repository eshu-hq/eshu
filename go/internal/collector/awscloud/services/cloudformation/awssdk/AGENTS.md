# AGENTS.md - internal/collector/awscloud/services/cloudformation/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - SDK pagination, the `apiClient` security boundary, and
   telemetry.
3. `mapping.go` - SDK-to-scanner mapping, parameter-key extraction, drift count
   accumulation.
4. `../scanner.go` - scanner-owned CloudFormation fact selection.
5. `../README.md` - CloudFormation scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and data boundaries.

## Invariants

- Keep CloudFormation SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Never add `GetTemplate`, `GetTemplateSummary`, `DescribeChangeSet`,
  `GetStackPolicy`, any `Detect*Drift` API, or any mutation API to `apiClient`.
  The guard test `TestAPIClientInterfaceExcludesTemplateAndMutationAPIs` exists
  to catch this; keep its forbidden list current.
- Drop parameter values during mapping; carry parameter keys only.
- Never carry the stack-set `TemplateBody` into the scanner type.
- Reduce drift results to per-status counts; never carry property documents.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new CloudFormation metadata read by extending `cloudformation.Client`,
  writing a scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not reveal a
  template body, parameter value, change-set body, or drift property document.

## What Not To Change Without An ADR

- Do not read template bodies, parameter values, change-set bodies, stack
  policies, or drift property documents.
- Do not trigger drift detection (`Detect*Drift`).
- Do not infer workload, environment, deployment, or ownership truth from stack
  names, tags, or relationships.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
