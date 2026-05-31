# AGENTS.md - internal/collector/awscloud/services/verifiedpermissions/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Verified Permissions SDK pagination and telemetry.
3. `mapper.go` - safe metadata mapping of policy stores, policies, identity
   sources, and the configuration/encryption unions.
4. `exclusion_test.go` - the build-time gate that fails if a Cedar-body read,
   authorization-evaluation call, or mutation method reaches the adapter
   interface.
5. `../scanner.go` - scanner-owned Verified Permissions fact selection.
6. `../README.md` - Verified Permissions scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Verified Permissions SDK calls here, not in `cmd/collector-aws-cloud` or
  the scanner package.
- Keep the `apiClient` interface limited to the four allowed reads
  (`ListPolicyStores`, `GetPolicyStore`, `ListPolicies`,
  `ListIdentitySources`). The exclusion test fails the build if any method is
  not one of those or matches a body-read, authorization, or mutation name; do
  not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe policy store, policy, and identity source metadata.
  Map the encryption configuration to a `DEFAULT`/`KMS` label only - never the
  KMS key ARN or encryption context. Map identity source configuration to the
  provider kind plus the non-secret provider reference; record an application
  client id count, never the client id values.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Verified Permissions metadata read by extending `Client` and the
  `apiClient` interface with another allowed read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any body-read, authorization, or mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal a Cedar body, schema body, template body, or client secret.

## What Not To Change Without An ADR

- Do not read Cedar policy statement bodies, schema bodies, or policy template
  bodies, evaluate authorization requests, or call any Verified Permissions
  mutation API.
- Do not persist the KMS key ARN, encryption context, or application client id
  values.
- Do not infer workload, environment, deployment, or ownership truth from
  Verified Permissions ids or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
