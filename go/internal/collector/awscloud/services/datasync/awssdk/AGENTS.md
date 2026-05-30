# AGENTS.md - internal/collector/awscloud/services/datasync/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - DataSync SDK pagination, describe dispatch, and telemetry.
3. `mapper.go` - location URI parsing and small mapping helpers.
4. `../scanner.go` - scanner-owned DataSync fact selection.
5. `../README.md` - DataSync scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataSync SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Keep the `apiClient` seam read-only: only List and Describe operations. Never
  add a Create/Start/Cancel/Update/Delete or task-execution method.
- Map only safe location metadata. Never propagate object-storage access keys,
  server certificates, SMB/object-storage passwords, or include/exclude filter
  patterns into scanner-owned types.
- Forward the FSx for NetApp ONTAP `FsxFilesystemArn` directly; do not
  synthesize ARNs in the adapter. ARN synthesis is the scanner's
  partition-aware responsibility.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new DataSync metadata read by extending `apiClient` and `Client`,
  writing a scanner or adapter test first, then mapping the SDK response into
  scanner-owned types.
- Add a new location flavor by adding its describe dispatch case and the URI
  parser, after confirming the URI scheme and backing-identity field from the
  AWS SDK.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not run transfers, mutate any DataSync resource, or call any task-execution
  read.
- Do not read object-storage access keys, server certificates, or storage
  passwords.
- Do not infer workload, environment, deployment, or ownership truth from
  DataSync names or URIs.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
