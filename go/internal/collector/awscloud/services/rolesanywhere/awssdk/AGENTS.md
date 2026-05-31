# AGENTS.md - internal/collector/awscloud/services/rolesanywhere/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Roles Anywhere SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a body-read,
   credential-read, or mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Roles Anywhere fact selection.
5. `../README.md` - Roles Anywhere scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Roles Anywhere SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a body-read,
  credential-read, or mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe trust-anchor, profile, and CRL metadata plus resource tags.
  Never read or persist certificate material, CRL body bytes, session policy
  documents, attribute-mapping rule contents, or session credentials.
- `trustAnchorSource` must ignore the `CERTIFICATE_BUNDLE` PEM x509 data and only
  extract the source type and the `AWS_ACM_PCA` CA ARN.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Roles Anywhere metadata read by extending `Client` and the
  `apiClient` interface with another `List*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal certificate material, a CRL body, a policy document, or credentials.

## What Not To Change Without An ADR

- Do not read CRL bodies, certificate material, subjects, or credentials, run
  any mutation, or call any Roles Anywhere mutation API.
- Do not infer workload, environment, deployment, or ownership truth from Roles
  Anywhere names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
