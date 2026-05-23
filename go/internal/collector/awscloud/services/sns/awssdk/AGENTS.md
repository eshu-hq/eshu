# AGENTS.md - internal/collector/awscloud/services/sns/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - SNS SDK pagination, safe attribute mapping, endpoint
   redaction, and telemetry.
3. `../scanner.go` - scanner-owned SNS fact selection.
4. `../README.md` - SNS scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SNS SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe topic metadata attributes. Do not persist policy,
  delivery-policy, or data-protection-policy JSON.
- Do not call message-content or mutation APIs such as Publish, Subscribe,
  Unsubscribe, SetTopicAttributes, or PutDataProtectionPolicy.
- Do not persist raw non-ARN subscription endpoints.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new SNS metadata read by extending `sns.Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend topic mapping only for AWS source data that is metadata and does not
  reveal message payloads, policy JSON, or subscriber PII.

## What Not To Change Without An ADR

- Do not publish, subscribe, unsubscribe, or mutate SNS resources.
- Do not infer workload, environment, deployment, or ownership truth from topic
  names, tags, or subscriptions.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
