# AGENTS.md - internal/collector/awscloud/services/ecs guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ECS domain types.
3. `scanner.go` - cluster, service, task-definition, task, and relationship
   emission.
4. `image_reference.go` - running-task container `aws_image_reference`
   emission and the ECR image-host parser.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - ECS slice
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
- Preserve ECS task ENI IDs from task describe responses so EC2 topology can join tasks
  to subnets and VPCs later.
- Keep task-definition env values, secret refs, resource ARNs, tags, and image
  refs out of metric labels.

## Common Changes

- Add a new ECS resource by extending the scanner-owned type, writing a focused
  scanner test first, then mapping it through `awscloud` envelope builders.
- Add new task-definition fields only when the ECS API reports them directly
  and the field is safe for persistence.
- Extend SDK pagination in the `awssdk` adapter, not here.
- Extend `image_reference.go` only for another AWS-registry-shaped image host
  pattern (for example a new ECR partition). Do not widen it to force a
  non-AWS-registry image into the `aws_image_reference` shape; that is a
  documented bounded gap (see README "Gotchas / invariants"), not a bug.

## What Not To Change Without An ADR

- Do not resolve ECS services or images to source repositories here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
