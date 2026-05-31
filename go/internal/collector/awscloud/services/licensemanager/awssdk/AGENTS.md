# AGENTS.md - internal/collector/awscloud/services/licensemanager/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - License Manager SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a checkout or
   mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned License Manager fact selection.
5. `../README.md` - License Manager scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep License Manager SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a checkout/
  entitlement/mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe license-configuration metadata, association metadata, and
  resource tags. Never read or persist an entitlement token, checkout record, or
  usage measurement.
- Distinguish an absent managed license count (nil) from a real zero via
  `LicenseCountConfigured`; never fabricate a count.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new License Manager metadata read by extending `Client` and the
  `apiClient` interface with another `List*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal an entitlement token or usage record.

## What Not To Change Without An ADR

- Do not grant, check out, check in, or mutate a license, or call any License
  Manager mutation API.
- Do not read or persist an entitlement token, access token, or usage record.
- Do not infer workload, environment, deployment, or ownership truth from
  License Manager names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
