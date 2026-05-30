# AGENTS.md - internal/collector/awscloud/services/transfer/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Transfer SDK pagination, safe metadata mapping, and telemetry.
3. `exclusion_test.go` - the reflective metadata-only acceptance gate.
4. `../scanner.go` - scanner-owned Transfer fact selection.
5. `../README.md` - Transfer scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Transfer SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe server and user metadata. Never copy `HostKeyFingerprint`,
  login banners, `SshPublicKeys`, `Policy`, or `PosixProfile` into
  scanner-owned types.
- Keep the `apiClient` interface restricted to `List*` and `Describe*` reads.
  The exclusion test fails the build if a mutation, lifecycle, or key-material
  method appears.
- Forward only home-directory mapping `Entry` and `Target` paths. Never read
  object or file contents.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Transfer metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does
  not reveal key material, policy bodies, or credential content.

## What Not To Change Without An ADR

- Do not add any `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*`, `Import*`,
  `Test*`, or other mutation/key-material method to the `apiClient` interface.
- Do not copy host key fingerprints, host key material, SSH public key bodies,
  user policy JSON, or POSIX UID/GID material into scanner-owned types.
- Do not infer workload, environment, deployment, or ownership truth from
  Transfer names, paths, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
