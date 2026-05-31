# AWS Application Auto Scaling Scanner

## Purpose

`internal/collector/awscloud/services/applicationautoscaling` owns the AWS
Application Auto Scaling scanner contract for the AWS cloud collector. It
converts scalable target, scaling policy, and scheduled action metadata into
`aws_resource` facts and emits relationship evidence joining a scalable target
to the resource it governs, a scaling policy to its CloudWatch alarms, and
policies and scheduled actions to their scalable target.

Application Auto Scaling is the cross-service scaling control plane for
DynamoDB, ECS, Aurora (RDS), Lambda provisioned concurrency, AppStream, Spot
Fleet, SageMaker, Comprehend, Keyspaces (Cassandra), MSK (Kafka), ElastiCache,
Neptune, EMR, WorkSpaces, and custom resources. The scanner iterates every
supported `ServiceNamespace` because each Describe call requires one.

## Resources and resource_id shapes

- `aws_application_autoscaling_scalable_target` — `resource_id` is the composite
  `<namespace>/<scalable_dimension>/<resource_id>` triple that uniquely
  identifies a registered target (Application Auto Scaling has no single ARN per
  target across all namespaces).
- `aws_application_autoscaling_scaling_policy` — `resource_id` is the policy ARN,
  falling back to a composite when AWS omits it.
- `aws_application_autoscaling_scheduled_action` — `resource_id` is the action
  ARN, falling back to a composite when AWS omits it.

## Relationships and join keys

- `application_autoscaling_target_scales_dynamodb_table` →
  `aws_dynamodb_table` by the partition-aware `arn:<partition>:dynamodb:<region>:<account>:table/<name>`
  the DynamoDB scanner publishes. A global secondary index target
  (`table/<t>/index/<i>`) has no dedicated node and is skipped.
- `application_autoscaling_target_scales_ecs_service` → `aws_ecs_service` by the
  long-format `arn:<partition>:ecs:<region>:<account>:service/<cluster>/<service>`.
- `application_autoscaling_target_scales_rds_cluster` → `aws_rds_db_cluster` by
  `arn:<partition>:rds:<region>:<account>:cluster:<name>`.
- `application_autoscaling_target_scales_lambda_function` →
  `aws_lambda_function` by the base `arn:<partition>:lambda:<region>:<account>:function:<name>`
  (the version/alias qualifier from the scaling resource id is dropped to match
  the function node).
- `application_autoscaling_policy_triggers_cloudwatch_alarm` →
  `aws_cloudwatch_alarm` by the alarm ARN Application Auto Scaling reports.
- `application_autoscaling_policy_for_scalable_target` and
  `application_autoscaling_scheduled_action_for_scalable_target` →
  `aws_application_autoscaling_scalable_target` by the same composite triple the
  scalable-target node publishes.

Namespaces whose governed resource is not scanned to a stable ARN-keyed node
(Spot Fleet, AppStream, SageMaker, Comprehend, Cassandra, Kafka, ElastiCache,
Neptune, EMR, WorkSpaces, custom resources) emit the scalable-target node but no
scale edge, so the graph never carries a dangling join.

## Ownership boundary

This package owns scanner-level Application Auto Scaling fact selection and
identity mapping. It does not own AWS SDK pagination, STS credentials, workflow
claims, fact envelope schema, or reducer projection. The `awssdk` subpackage
owns the SDK adapter; `runtimebind` registers the scanner.

## Metadata-only guarantees

The scanner reads only `DescribeScalableTargets`, `DescribeScalingPolicies`, and
`DescribeScheduledActions`. It never registers, deregisters, mutates, or invokes
a scaling action; the adapter read surface excludes those operations by
construction, proven by a reflection guard test. Step-scaling and
target-tracking configuration bodies are never persisted; only the bound
CloudWatch alarm ARNs are kept. Synthesized ARNs derive their partition from the
scan boundary, never a hardcoded `arn:aws:`.

## Evidence

No-Regression Evidence: metadata-only control-plane scanner; new read path, no change to existing hot paths. `go test ./internal/collector/awscloud/services/applicationautoscaling/...` green.
No-Observability-Change: reuses shared AWS pagination span + API-call/throttle counters; no telemetry contract change.
