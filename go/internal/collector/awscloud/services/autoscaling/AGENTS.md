# AGENTS.md - internal/collector/awscloud/services/autoscaling guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Auto Scaling domain types.
3. `scanner.go` - group, launch-configuration, scaling-policy, lifecycle-hook,
   scheduled-action, and relationship emission.
4. `relationships.go` - relationship target-type and join-key construction.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - Auto Scaling slice
   requirements.

## Invariants

- Keep the Auto Scaling group resource_id as the bare group name. CodeDeploy
  emits `codedeploy_deployment_group_targets_auto_scaling_group` edges keyed on
  the group name; changing the resource_id to the ARN re-breaks those edges.
- Keep Auto Scaling API access behind `Client`; do not import the AWS SDK into
  this package.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from group names, launch configuration names, or tags.
- Never persist launch configuration or launch template UserData. The
  `LaunchConfiguration` type must not declare a UserData field (or any
  launch-detail field); it carries identity only.
- Never persist lifecycle-hook `NotificationMetadata`. The `LifecycleHook` type
  must not declare that field.
- The scanner must never mutate an Auto Scaling resource, set desired capacity,
  or terminate instances. The adapter read surface enforces this.
- Every relationship must set a non-empty `target_type` matching the target
  scanner's `resource_id` form. Do not emit empty target types.
- Wrap client errors with `%w`; never swallow partial failures.

## Common Changes

- Add a new Auto Scaling resource by extending the scanner-owned type, writing a
  focused scanner test first, then mapping it through `awscloud` envelope
  builders.
- Add new group fields only when the Auto Scaling API reports them directly and
  the field is safe for persistence.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not change the Auto Scaling group resource_id form.
- Do not resolve groups, launch configurations, or images to source
  repositories here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not add a UserData field or a NotificationMetadata field.
