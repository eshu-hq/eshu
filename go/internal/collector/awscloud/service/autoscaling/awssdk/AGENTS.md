# AGENTS.md - services/autoscaling/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - pagination, per-group lifecycle-hook fan-out, and telemetry
   wiring.
3. `mapping.go` - SDK-to-scanner record mapping.
4. `exclusion_test.go` - the metadata-only read-surface guard.
5. `../README.md` - Auto Scaling scanner contract.

## Invariants

- Keep the `apiClient` interface Describe-only. Never add CreateAutoScalingGroup,
  UpdateAutoScalingGroup, DeleteAutoScalingGroup, SetDesiredCapacity,
  TerminateInstanceInAutoScalingGroup, or any Create/Update/Delete/Set
  operation. `exclusion_test.go` enforces this; do not weaken it.
- Never read launch configuration or launch template UserData in
  `mapLaunchConfiguration` or `mapGroup`. The scanner-owned types do not declare
  a UserData field, so a leak would not compile.
- Never carry lifecycle-hook `NotificationMetadata` out of `mapLifecycleHook`.
- Keep `splitSubnetIdentifier` returning bare subnet IDs and `mapGroup`
  preferring the launch template ID so the edge join keys match the EC2-owned
  resource_id forms.
- Record every API call through `recordAPICall` so telemetry stays consistent
  with the other AWS adapters. Keep ARNs, tags, and caller data out of metric
  labels.

## Common Changes

- Add a new Auto Scaling read by extending `apiClient`, paginating in
  `client.go`, and mapping the response in `mapping.go`. Add the field to the
  scanner-owned type in the parent package first.
- Extend pagination only with a performance note in `README.md`.

## What Not To Change Without An ADR

- Do not add a mutation or capacity-control API to `apiClient`.
- Do not add a UserData or NotificationMetadata field to the scanner-owned
  types.
- Do not add credential loading or STS calls here.
