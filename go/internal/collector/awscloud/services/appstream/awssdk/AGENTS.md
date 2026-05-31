# AGENTS.md - internal/collector/awscloud/services/appstream/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - AppStream SDK pagination, association reads, and telemetry.
3. `mapping.go` - safe SDK-to-scanner metadata mapping.
4. `exclusion_test.go` - the build-time gate that fails if a session, credential,
   or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned AppStream fact selection.
6. `../README.md` - AppStream scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppStream SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to `Describe*` and `List*` reads. The
  exclusion test fails the build if any method is a session/credential read or a
  mutation; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe fleet, stack, image builder, and image metadata plus
  resource tags. Never read or persist session, user, session-script, or
  streaming-URL credential content.
- Scope `DescribeImages` to PRIVATE and SHARED visibility; never scan the
  AWS-managed PUBLIC base-image catalog.
- Copy only the HOMEFOLDERS storage-connector bucket name and the
  application-settings bucket name; Google Drive and OneDrive connectors carry
  domains, not buckets.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new AppStream metadata read by extending `Client` and the `apiClient`
  interface with another `Describe*` or `List*` read, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any session, credential, or mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal session, user, or credential content.

## What Not To Change Without An ADR

- Do not read sessions or users, mint streaming URLs, or call any AppStream
  mutation API.
- Do not scan PUBLIC images.
- Do not infer workload, environment, deployment, or ownership truth from
  AppStream names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
