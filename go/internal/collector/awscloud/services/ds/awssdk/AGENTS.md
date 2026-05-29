# AGENTS.md - internal/collector/awscloud/services/ds/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Directory Service SDK pagination, per-directory fan-out, and
   telemetry.
3. `mapping.go` - SDK-to-scanner mapping and the secret-exclusion boundary.
4. `client_test.go` - the metadata-only `apiClient` reflection guard and the
   mapping, pagination, and LDAPS-skip tests.
5. `../scanner.go` - scanner-owned Directory Service fact selection.
6. `../README.md` - Directory Service scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Directory Service SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Keep the `apiClient` interface limited to describe-class metadata reads. Any
  added method must keep `TestAPIClientInterfaceExcludesMutationAndSecretAPIs`
  green.
- Do not call any mutation API (ResetUserPassword, Create/Delete/Update/Enable/
  Disable/Register/Accept/Reject/Share/...).
- NEVER map the directory admin password, the RADIUS shared secret
  (`RadiusSettings`), or the AD Connector service-account user name
  (`ConnectSettings.CustomerUserName`).
- Query LDAPS only for Managed Microsoft AD directory types; Simple AD and AD
  Connector do not support LDAPS.
- Guard each paginator loop against a nil page before iterating and follow the
  `NextToken` cursor to completion.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Directory Service metadata read by extending `ds.Client` and the
  `apiClient` interface, writing the reflection guard and a mapping test first,
  then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not reveal a
  password, the RADIUS shared secret, or service-account credentials.

## What Not To Change Without An ADR

- Do not map any directory admin password, RADIUS shared secret, or AD Connector
  service-account field.
- Do not infer workload, environment, deployment, or ownership truth from
  directory names, domain names, tags, subnets, or trusts.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
