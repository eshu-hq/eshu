# AGENTS.md - internal/collector/awscloud/services/workspaces/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - WorkSpaces SDK pagination and telemetry.
3. `mappers.go` - safe SDK-to-scanner metadata mapping.
4. `exclusion_test.go` - the build-time gate that fails if a session,
   connection-status, credential, or mutation method reaches the adapter
   interface.
5. `../scanner.go` - scanner-owned WorkSpaces fact selection.
6. `../README.md` - WorkSpaces scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep WorkSpaces SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `Describe*` reads. The exclusion
  test fails the build if any method is not a `Describe` read or matches a
  session/connection/credential/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe metadata plus resource tags. Never read or persist passwords,
  registration codes, WorkSpace IP addresses, connection state, or session
  content. The mappers must drop those fields even though the SDK returns them.
- Page bundles without an `Owner` filter so the account's own bundles are read;
  do not enumerate the AMAZON-provided catalog.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new WorkSpaces metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*` read, writing a scanner or adapter test
  first, then mapping the SDK response into scanner-owned types. The exclusion
  test rejects any non-`Describe` addition or any session/credential read.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal a credential, registration code, IP address, or session detail.

## What Not To Change Without An ADR

- Do not read session contents, connection status, snapshots, image
  permissions, client branding, or credentials, and do not call any WorkSpaces
  mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  WorkSpaces names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
