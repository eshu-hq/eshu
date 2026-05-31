# AGENTS.md - internal/collector/awscloud/services/cloudhsmv2/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - CloudHSM v2 SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a mutation,
   initialize, or resource-policy method reaches the adapter interface.
4. `../scanner.go` - scanner-owned CloudHSM v2 fact selection.
5. `../README.md` - CloudHSM v2 scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep CloudHSM v2 SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `Describe*` reads. The exclusion
  test fails the build if any method is not a `Describe` read or matches a
  mutation/initialize/policy name; do not loosen it.
- Wrap each AWS paginator page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe cluster and backup metadata plus inline resource tags. Never
  read or persist key material, certificate bodies, CSR bodies, or the
  Pre-Crypto Officer password. Inspect certificate/CSR fields only to test
  presence.
- Do not map the `PreCoPassword` field.

## Common Changes

- Add a new CloudHSM v2 metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*` read, writing a scanner or adapter test
  first, then mapping the SDK response into scanner-owned types. The exclusion
  test rejects any non-`Describe` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal key material, a certificate body, a CSR body, or the PRECO
  password.

## What Not To Change Without An ADR

- Do not call any CloudHSM v2 mutation, InitializeCluster, or resource-policy
  API.
- Do not read or map key material, certificate bodies, CSR bodies, or the
  Pre-Crypto Officer password.
- Do not infer workload, environment, deployment, or ownership truth from
  CloudHSM v2 names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
