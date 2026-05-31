# AGENTS.md - internal/collector/awscloud/services/grafana/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Grafana SDK pagination, safe metadata mapping, and telemetry.
3. `helpers.go` - partition-aware workspace ARN synthesis.
4. `exclusion_test.go` - the build-time gate that fails if an auth-read, key/
   token, or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned Grafana fact selection.
6. `../README.md` - Grafana scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Managed Grafana SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` and `Describe*` reads. The
  exclusion test fails the build if any method is not a `List`/`Describe` read or
  matches an authentication-read, key/token, license, or mutation name; do not
  loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Persist only safe workspace metadata plus resource tags. Never read or persist
  SAML / IAM Identity Center authentication configuration, workspace API keys,
  service-account tokens, dashboards, alert rules, or query results.
- Synthesize the workspace ARN with `awscloud.PartitionForBoundary`; never
  hardcode `arn:aws:`.
- Map data sources, notification destinations, and authentication providers as
  enum names only.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Grafana metadata read by extending `Client` and the `apiClient`
  interface with another `List*`/`Describe*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-read or auth/key/token/mutation addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal an authentication secret, API key, token, or data-plane payload.

## What Not To Change Without An ADR

- Do not read workspace authentication configuration, mint API keys or
  service-account tokens, associate licenses, or call any Grafana mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  Grafana names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
