# AGENTS.md - internal/collector/awscloud/services/ecs guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ECS domain types.
3. `scanner.go` - cluster, service, task-definition, task, and relationship
   emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - ECS slice
   requirements.

## Invariants

- Keep ECS API access behind `Client`; do not import the AWS SDK into this
  package.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from service names, task families, images, or tags.
- Redact task-definition environment values with `internal/redact` before they
  cross the scanner boundary.
- Preserve ECS secret `value_from` references and never attempt to resolve or
  read secret values.
- Keep task-definition env values, secret refs, resource ARNs, tags, and image
  refs out of metric labels.

## Common Changes

- Add a new ECS resource by extending the scanner-owned type, writing a focused
  scanner test first, then mapping it through `awscloud` envelope builders.
- Add new task-definition fields only when the ECS API reports them directly
  and the field is safe for persistence.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not resolve ECS services or images to source repositories here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
