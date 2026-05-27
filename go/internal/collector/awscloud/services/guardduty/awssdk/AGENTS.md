# AGENTS.md - internal/collector/awscloud/services/guardduty/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go`, `lists.go`, `sets.go`, and `ipsets.go` - GuardDuty SDK
   pagination and safe metadata mapping.
3. `../scanner.go` - scanner-owned GuardDuty fact selection.
4. `../README.md` - GuardDuty scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.

## Invariants

- Keep GuardDuty SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe detector, member, filter-name, publishing destination,
  aggregate count, threat intel set, and IP set metadata.
- Do not call ListFindings or GetFindings; finding bodies are out of scope.
- Do not call GetFilter; filter criteria expressions are out of scope.
- Do not add S3 clients or fetch threat intel/IP set location contents.
- Do not call GuardDuty mutation APIs.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new GuardDuty metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Keep every new field on the metadata side of the issue #717 boundary.

## What Not To Change Without An ADR

- Do not read finding bodies, resolve S3 list locations, inspect filter
  criteria, or call mutation APIs.
- Do not infer workload, environment, deployment, ownership, attacker identity,
  or defender posture truth from GuardDuty metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
