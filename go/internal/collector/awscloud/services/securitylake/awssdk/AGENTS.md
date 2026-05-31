# AGENTS.md - internal/collector/awscloud/services/securitylake/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Security Lake SDK pagination, safe metadata mapping, and
   telemetry.
3. `exclusion_test.go` - the build-time gate that fails if a credential-read or
   mutation method reaches the adapter interface.
4. `../scanner.go` - scanner-owned Security Lake fact selection.
5. `../README.md` - Security Lake scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Security Lake SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Keep the `apiClient` interface limited to `List*` reads. The exclusion test
  fails the build if any method is not a `List` read or matches a
  credential-read (`GetSubscriber`, notification/exception subscription) or
  mutation name; do not loosen it.
- Wrap each AWS list page in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe data lake, log source, and subscriber metadata. Never read
  or persist ingested records, the subscriber external id, or the subscriber
  endpoint. The `ExternalId` and `SubscriberEndpoint` SDK fields are dropped on
  purpose; do not start copying them.
- Page `ListLogSources` and `ListSubscribers` to exhaustion on `NextToken`.
- Expand the `LogSourceResource` union for both AWS-native and custom sources;
  record a custom source's log-provider role ARN only.

## Common Changes

- Add a new Security Lake metadata read by extending `Client` and the
  `apiClient` interface with another `List*` read, writing a scanner or adapter
  test first, then mapping the SDK response into scanner-owned types. The
  exclusion test rejects any non-`List` addition.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend resource mapping only for AWS source data that is metadata and does not
  reveal record content, a credential, an external id, or an endpoint.

## What Not To Change Without An ADR

- Do not read ingested records, read subscriber credentials (external id,
  endpoint), or call any Security Lake mutation API.
- Do not infer workload, environment, deployment, or ownership truth from
  Security Lake names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
