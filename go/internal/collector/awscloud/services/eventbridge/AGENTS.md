# AGENTS.md - internal/collector/awscloud/services/eventbridge guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned EventBridge domain types.
3. `scanner.go` - event bus, rule, and target relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep EventBridge API access behind `Client`; do not import the AWS SDK into
  this package.
- Never put events or mutate event buses, rules, or targets.
- Never persist event bus policy JSON.
- Never persist target input payloads, input paths, input transformers, or HTTP
  parameters.
- Never persist raw non-ARN target identities such as webhook URLs.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from bus names, rule names, or tags.
- Preserve stable event bus and rule identities across repeated observations in
  the same AWS generation.
- Keep event bus ARNs, rule ARNs, target ARNs, tags, and event patterns out of
  metric labels.

## Common Changes

- Add a new EventBridge metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when the EventBridge API reports both sides
  directly and the target identity is not sensitive.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not put events, delete rules, add targets, remove targets, or mutate
  EventBridge resources.
- Do not resolve bus names, rule names, tags, event patterns, or targets into
  workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
