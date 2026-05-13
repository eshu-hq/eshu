# AGENTS.md - internal/collector/awscloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - collector, service, resource, relationship, and observation
   contracts.
3. `envelope.go` - durable fact-envelope construction and validation.
4. Service package docs under `services/` before changing scanner-specific
   behavior.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   source-truth, claim, and credential contract.
6. `docs/docs/guides/collector-authoring.md` - general collector fact
   contract.

## Invariants

- AWS cloud data is reported source evidence. Do not materialize graph truth in
  this package.
- Keep the claim boundary explicit: account, region, service kind, scope,
  generation, collector instance, and fencing token.
- Preserve generation-specific `FactID` values and source-stable
  `StableFactKey` values.
- Never put secrets, session tokens, presigned URLs, full policies, tags, ARNs,
  or resource names in metric labels.
- Redact ECS task-definition environment values before persistence; preserve
  secret `value_from` references without resolving them.
- Keep AWS SDK calls out of this package. Runtime adapters own SDK pagination,
  retries, throttling, and credential loading.

## Common Changes

- Add a new AWS service by adding service constants here, a service package
  under `services/`, scanner tests, a service `awssdk` adapter, and a branch in
  `awsruntime.DefaultScannerFactory`.
- Add a new fact envelope only after `internal/facts` exposes the fact kind and
  schema version.
- Add redaction or credential rules at the runtime boundary unless the value is
  part of the durable envelope contract.

## What Not To Change Without An ADR

- Do not make this package call AWS APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, tags, folders, or account aliases in this package.
