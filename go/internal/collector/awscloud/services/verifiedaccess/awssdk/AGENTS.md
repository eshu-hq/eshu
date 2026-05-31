# AGENTS.md - internal/collector/awscloud/services/verifiedaccess/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Verified Access SDK pagination and telemetry.
3. `mapping.go` - safe SDK-to-scanner metadata mapping.
4. `exclusion_test.go` - the build-time gate that fails if a mutation, policy,
   or secret-read method reaches the adapter interface.
5. `../scanner.go` - scanner-owned Verified Access fact selection.
6. `../README.md` - Verified Access scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Verified Access SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `DescribeVerifiedAccess*` reads. The
  exclusion test fails the build if any method is not a Verified Access Describe
  read or matches a mutation/policy/secret name; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe instance, group, endpoint, and trust-provider metadata. From
  a trust provider copy only the OIDC issuer reference; never the client id,
  client secret, or token/userinfo endpoints. Copy only the SSE
  customer-managed-key flag, never the KMS key ARN.
- Do not read group/endpoint policy documents.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Verified Access metadata read by extending `Client` and the
  `apiClient` interface with another `DescribeVerifiedAccess*` read, writing a
  scanner or adapter test first, then mapping the SDK response into scanner-owned
  types. The exclusion test rejects any non-Describe addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a secret or policy body.

## What Not To Change Without An ADR

- Do not read policy documents, trust-provider secrets, or any data plane, and
  do not call any Verified Access mutation API.
- Do not copy OIDC client ids, client secrets, or token/userinfo endpoints.
- Do not infer workload, environment, deployment, or ownership truth from
  Verified Access names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
