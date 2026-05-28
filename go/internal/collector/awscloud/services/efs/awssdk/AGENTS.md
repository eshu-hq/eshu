# AGENTS.md - internal/collector/awscloud/services/efs/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - EFS SDK pagination, child-resource fanout, mapping, and
   telemetry.
3. `client_test.go` - the metadata-only `apiClient` reflection guard.
4. `../scanner.go` - scanner-owned EFS fact selection.
5. `../README.md` - EFS scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep EFS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to describe-class metadata reads. Any
  added method must keep `TestAPIClientInterfaceExcludesMutationAndPolicyAPIs`
  green.
- Do not call `DescribeFileSystemPolicy`, `DescribeBackupPolicy`, or any
  mutation API.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new EFS metadata read by extending `efs.Client` and the `apiClient`
  interface, writing the reflection-guard and a scanner or adapter test first,
  then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not reveal
  file contents or NFS file system policy bodies.

## What Not To Change Without An ADR

- Do not read, mount, or sample EFS file contents.
- Do not request, read, or persist file system policy bodies or backup policy
  bodies.
- Do not infer workload, environment, deployment, or ownership truth from file
  system names, tags, subnets, or security groups.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
