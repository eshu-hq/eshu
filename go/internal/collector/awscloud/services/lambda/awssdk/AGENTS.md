# AGENTS.md - internal/collector/awscloud/services/lambda/awssdk guidance

## Read First

1. `README.md` - adapter purpose, telemetry, and invariants.
2. `client.go` - Lambda SDK pagination, `GetFunction` enrichment, mapping, and
   telemetry.
3. `client_test.go` - response-mapping regression coverage.
4. Parent package docs in `../README.md` before changing emitted fields.

## Invariants

- Keep AWS SDK types inside this package.
- Keep `GetFunction.Code.Location` out of scanner records, facts, logs, spans,
  and tests.
- Keep raw environment values out of telemetry. They may only move to the
  parent scanner so it can emit redacted markers.
- Preserve Lambda image URI, resolved image URI, execution role ARN, VPC IDs,
  alias routing, and event-source ARNs exactly as AWS reports them.
- All AWS API calls must go through `recordAPICall` so metrics and throttle
  counters stay complete.

## Common Changes

- Add a mapped Lambda field in `mapFunction`, `mapAlias`, or
  `mapEventSourceMapping`.
- Add a focused mapper test before changing response normalization.
- Add API calls only when the parent scanner needs stable topology or
  correlation evidence.

## What Not To Change Without An ADR

- Do not fetch function code contents or logs.
- Do not persist presigned package URLs.
- Do not infer ownership, deployable units, or environments from Lambda names,
  aliases, tags, or event-source names.
