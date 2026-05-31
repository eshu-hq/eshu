# AGENTS.md - internal/collector/awscloud/services/pinpoint/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Pinpoint SDK pagination, safe metadata mapping, and telemetry.
3. `helpers.go` - ISO 8601 parsing and tag/clone helpers.
4. `exclusion_test.go` - the build-time gate that fails if an endpoint-read,
   message-send, or mutation method reaches the adapter interface.
5. `../scanner.go` - scanner-owned Pinpoint fact selection.
6. `../README.md` - Pinpoint scanner contract.
7. `../../../README.md` - AWS cloud envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Pinpoint SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Keep the `apiClient` interface limited to the four metadata reads `GetApps`,
  `GetSegments`, `GetChannels`, `GetEmailChannel`. The exclusion test fails the
  build if any method is not in that allowed set or matches an endpoint/send/
  mutation name; do not loosen it.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and result.
- Never copy the email `FromAddress` onto the scanner model; keep only the SES
  `ConfigurationSet` name and `Identity` ARN reference.
- Never copy the segment import S3 URL, external id, or role ARN; keep only the
  import presence flag, format, and aggregate size.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Pinpoint metadata read only when it is a metadata `Get*` that does
  not reveal endpoint, address, or message content. Add it to both the
  `apiClient` interface and the exclusion test's allowed set, writing a scanner
  or adapter test first.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.

## What Not To Change Without An ADR

- Do not read endpoint records, send messages, read message/template content,
  or call any Pinpoint mutation API.
- Do not persist the email from-address, the import S3 URL, the import external
  id, or segment targeting criteria values.
- Do not infer workload, environment, deployment, or ownership truth from
  Pinpoint names or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
