# AGENTS.md - internal/collector/awscloud/services/sqs guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned SQS domain types.
3. `scanner.go` - queue resource and dead-letter relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep SQS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read messages or persist message bodies.
- Never persist queue policy JSON.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from queue names or tags.
- Preserve stable queue identities across repeated observations in the same AWS
  generation.
- Keep queue URLs, ARNs, tags, and redrive policy values out of metric labels.

## Common Changes

- Add a new SQS metadata field by extending `QueueAttributes`, writing a focused
  scanner or adapter test first, then mapping it through `awscloud` envelope
  builders.
- Add new relationship evidence only when the SQS API reports both sides
  directly.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or sample queue messages.
- Do not resolve queue names, tags, or DLQ links into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
