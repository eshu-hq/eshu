# AGENTS.md - internal/collector/awscloud/services/controltower/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Control Tower SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a mutation method
   reaches the adapter interface.
4. `../scanner.go` - scanner-owned Control Tower fact selection.
5. `../README.md` - Control Tower scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Control Tower SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to the five audited reads
  (`ListLandingZones`, `GetLandingZone`, `ListEnabledControls`,
  `ListEnabledBaselines`, `ListTagsForResource`). The exclusion test fails the
  build if any method is not a `List`/`Get` read or matches a mutation prefix;
  do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Map only safe landing-zone, control, and baseline metadata plus resource
  tags. Never copy the landing-zone manifest body, control parameter values, or
  baseline parameter values.
- `ListEnabledControls` needs a target; derive distinct targets from the enabled
  baselines and de-duplicate enabled controls by ARN.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Control Tower metadata read by extending `Client` and the
  `apiClient` interface with another audited `List`/`Get` read, writing a
  scanner or adapter test first, then mapping the SDK response into
  scanner-owned types. The exclusion test rejects any mutation-prefixed
  addition and any method outside the audited allow set.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a manifest, parameter, or governance payload.

## What Not To Change Without An ADR

- Do not read the landing-zone manifest, control parameters, baseline
  parameters, operation results, or any governance payload, and do not call any
  Control Tower mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  Control Tower identifiers or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
