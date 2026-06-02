# AGENTS.md - internal/collector/awscloud/services/computeoptimizer guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Compute Optimizer domain types.
3. `scanner.go` - summary and recommendation resource and relationship emission.
4. `observations.go` - resource-observation builders per recommendation type.
5. `relationships.go` - relationship emission rules and join keys.
6. `helpers.go` - resource_id derivation, bare-id extraction from ARNs, and
   scanner-side cloning helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Compute Optimizer API access behind `Client`; do not import the AWS SDK
  into this package.
- Never mutate Compute Optimizer state. Never call `UpdateEnrollmentStatus`,
  `PutRecommendationPreferences`, `DeleteRecommendationPreferences`, or any
  `Export*` start. Never persist the CloudWatch utilization metric data points
  behind a recommendation.
- An account not enrolled in Compute Optimizer is not an error. Return an empty
  snapshot so the scan completes cleanly.
- Each per-resource recommendation node publishes the analyzed resource ARN as
  its resource_id. Source a recommendation's own edge on that exact value.
- Key the recommendation-to-instance edge on the BARE EC2 instance id (`i-...`),
  extracted from the analyzed instance ARN, with `target_arn` unset. EC2
  instance relationship targets are published by bare id.
- Key the recommendation-to-Auto-Scaling-group edge on the group NAME (not the
  ARN), with `target_arn` unset, because the autoscaling scanner publishes its
  group resource_id as the bare name.
- Key the recommendation-to-function edge on the function ARN, matching the
  lambda scanner's published function resource_id.
- Do NOT key an EBS volume edge in this scanner until the dedicated
  recommendation-to-`aws_ec2_volume` relationship follow-up lands. Record the
  volume identity as metadata only.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant or the documented `aws_ec2_instance`
  forward-reference anchor, and a `target_resource_id` matching how the target
  scanner publishes its resource_id. If `target_arn` is set, `target_resource_id`
  must also be an ARN (relguard enforces this).
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from findings or AWS tags.
- Keep recommendation ARNs, names, findings, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Compute Optimizer metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field carries CloudWatch metric data
  points or customer cost values, leave it out of the scanner contract.
- Add new relationship evidence only when the Compute Optimizer API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (bare id for EC2 instances, group name for Auto Scaling
  groups, ARN for Lambda functions).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not change enrollment, write recommendation preferences, start exports, or
  call any Compute Optimizer mutation API.
- Do not read or persist CloudWatch utilization metric data points or customer
  cost data points.
- Do not resolve Compute Optimizer findings or tags into workload ownership
  here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
