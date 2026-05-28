# AGENTS.md - internal/collector/awscloud/services/fsx/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - FSx SDK pagination and telemetry.
3. `mapping.go` - SDK-to-scanner mapping and the credential-exclusion boundary.
4. `client_test.go` - the metadata-only `apiClient` reflection guard and the
   per-flavor mapping and redaction tests.
5. `../scanner.go` - scanner-owned FSx fact selection.
6. `../README.md` - FSx scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep FSx SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` interface limited to describe-class metadata reads. Any
  added method must keep `TestAPIClientInterfaceExcludesMutationAndContentAPIs`
  green.
- Do not call any mutation API (Create/Delete/Update/Restore/Copy/Release) and
  do not read file contents or aliases.
- NEVER map Active Directory self-managed credentials (Password, UserName,
  FileSystemAdministratorsGroup, DnsIps, DomainJoinServiceAccountSecret), the
  ONTAP fsxadmin password, or the SVM admin password. The mapping layer reads
  only the AWS Managed Microsoft AD directory ID from AD configuration.
- Guard each paginator loop against a nil page before iterating.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new FSx metadata read by extending `fsx.Client` and the `apiClient`
  interface, writing the reflection guard and a mapping test first, then mapping
  the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend mapping only for AWS source data that is metadata and does not reveal
  file contents or AD/SVM credentials.

## What Not To Change Without An ADR

- Do not read, mount, or sample FSx file contents.
- Do not map any AD self-managed credential field, the fsxadmin password, or the
  SVM admin password.
- Do not infer workload, environment, deployment, or ownership truth from file
  system names, mount paths, tags, subnets, or directories.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
