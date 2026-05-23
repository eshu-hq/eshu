# AGENTS.md - internal/collector/awscloud/services/eventbridge/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - EventBridge SDK pagination, safe target mapping, and telemetry.
3. `../scanner.go` - scanner-owned EventBridge fact selection.
4. `../README.md` - EventBridge scanner contract.
5. `../../../README.md` - AWS cloud envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep EventBridge SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe event bus, rule, tag, and target metadata.
- Do not persist event bus policy JSON.
- Do not persist target `Input`, `InputPath`, `InputTransformer`, or
  `HttpParameters`.
- Do not call event payload or mutation APIs such as PutEvents, PutRule,
  PutTargets, DeleteRule, or RemoveTargets.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new EventBridge metadata read by extending `Client`, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy evidence
  shows the code is retry/throttle-shaped.
- Extend target mapping only for AWS source data that is metadata and does not
  reveal payload content, headers, query parameters, policy JSON, or secret
  material.

## What Not To Change Without An ADR

- Do not put events, mutate rules, mutate targets, read archives/replays, or
  inspect connection/API destination secrets.
- Do not infer workload, environment, deployment, or ownership truth from bus
  names, rule names, tags, event patterns, or targets.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
