# AGENTS.md - internal/collector/awscloud/services/signer/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Signer SDK pagination, safe metadata mapping, and telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a signing-job read,
   sign action, or mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Signer fact selection.
5. `../README.md` - Signer scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Signer SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `List*` and `Get*` reads. The
  exclusion test fails the build if any method is not a `List`/`Get` read or
  matches a job/sign/permission/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe signing-profile and signing-platform metadata. Never read or
  persist signing material private keys, signed-object payloads, signing-job
  data, revocation records, or signing-parameter values.
- Copy only signing-parameter NAMES and the ACM certificate ARN reference.
- Do not call any signing-job read (`ListSigningJobs`, `DescribeSigningJob`),
  `GetRevocationStatus`, profile-permission API, or any Start/Sign/Put/Cancel/
  Revoke/Add/Remove/Tag mutation API.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Signer metadata read by extending `Client` and the `apiClient`
  interface with another `List*` or `Get*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any addition that is not a `List`/`Get` read or that
  matches a forbidden name.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal signing material, signed payloads, or signing-parameter values.

## What Not To Change Without An ADR

- Do not start signing jobs, read signing material, read signed objects, read
  signing-job data, or call any Signer mutation/signing API.
- Do not persist signing-parameter values, revocation records, or per-job data.
- Do not infer workload, environment, deployment, or ownership truth from Signer
  names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
