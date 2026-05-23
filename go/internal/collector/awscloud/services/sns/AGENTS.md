# AGENTS.md - internal/collector/awscloud/services/sns guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned SNS domain types.
3. `scanner.go` - topic resource and subscription relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SNS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never publish messages or persist message payloads.
- Never persist topic policy JSON, delivery-policy JSON, or
  data-protection-policy JSON.
- Never persist raw email, SMS, HTTP, or HTTPS subscription endpoints.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from topic names or tags.
- Preserve stable topic identities across repeated observations in the same AWS
  generation.
- Keep topic ARNs, tags, subscription ARNs, and endpoints out of metric labels.

## Common Changes

- Add a new SNS metadata field by extending `TopicAttributes`, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when the SNS API reports both sides
  directly and the target identity is not sensitive.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not publish messages, subscribe endpoints, unsubscribe endpoints, or mutate
  topics.
- Do not resolve topic names, tags, or subscriptions into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
