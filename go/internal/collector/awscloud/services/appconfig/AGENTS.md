# AGENTS.md - internal/collector/awscloud/services/appconfig guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned AppConfig domain types.
3. `scanner.go` - application, environment, configuration profile, and
   deployment strategy resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - partition-aware ARN synthesis and scanner-side cloning
   helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppConfig API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read configuration content, hosted configuration version bodies, or
  freeform/feature-flag values. Never call `GetConfiguration` or
  `GetLatestConfiguration` (the appconfigdata module is never imported),
  `GetHostedConfigurationVersion`, any `StartDeployment`/`StopDeployment`, or
  any `Create*`, `Update*`, `Delete*` mutation API.
- AppConfig list responses carry no ARN. Synthesize the partition-aware
  application/environment/profile/deployment-strategy ARN with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud and
  China must resolve to the real node. Each node publishes its synthesized ARN
  as its resource_id.
- Key the environment-in-application and profile-in-application edges on the
  application ARN the application node publishes so they join the application
  node.
- Source the environment monitor edges on the environment ARN, the resource_id
  the environment node publishes.
- Emit the environment-to-CloudWatch-alarm edge only when AppConfig reports an
  alarm ARN; target it by that ARN, matching the CloudWatch scanner's published
  alarm resource_id.
- Emit the environment-to-IAM-role edge only when AppConfig reports an
  ARN-shaped role identifier, matching the IAM scanner's published role
  resource_id.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from application,
  environment, profile, or strategy names, or AWS tags.
- Preserve stable identities across repeated observations in the same AWS
  generation.
- Keep AppConfig ARNs, names, and AWS error payloads out of metric labels.

## Common Changes

- Add a new AppConfig metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry configuration content,
  leave it out of the scanner contract.
- Add new relationship evidence only when the AppConfig API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (the CloudWatch alarm ARN, the IAM role ARN, the
  synthesized application ARN for the parent application).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read configuration content, hosted configuration version bodies,
  freeform/feature-flag values, start or stop deployments, or call any AppConfig
  mutation API.
- Do not add the appconfigdata module to this package's dependency graph.
- Do not resolve AppConfig names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
