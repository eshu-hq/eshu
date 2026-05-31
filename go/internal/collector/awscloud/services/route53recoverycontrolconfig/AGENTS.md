# AGENTS.md - internal/collector/awscloud/services/route53recoverycontrolconfig guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned recovery-control domain types.
3. `scanner.go` - cluster, control panel, routing control, and safety rule
   resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep recovery-control API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read or set the live routing control On/Off state. Never call
  `UpdateRoutingControlState` (the route53recoverycluster data-plane module is
  never imported) or any `Create*`, `Update*`, `Delete*` mutation API.
- Every node publishes its resource_id as the AWS-reported ARN (fallback to the
  resource name). Key each membership edge on the parent's ARN so it joins the
  cluster or control panel node this scanner emits.
- All three relationships are internal to this scanner: control-panel-in-cluster
  targets `aws_route53recoverycontrolconfig_cluster`, and both
  routing-control-in-control-panel and safety-rule-in-control-panel target
  `aws_route53recoverycontrolconfig_control_panel`. Set `target_arn` only when
  the join key is ARN-shaped.
- Route 53 ARC is global. Use API-reported ARNs directly as join keys; never
  hardcode or synthesize `arn:aws:`, so GovCloud and China resources resolve.
- Drop cluster endpoint URLs; keep only the endpoint Region names. The URLs are
  handles to the routing control state data plane.
- Safety rules record rule logic and routing control counts only, never traffic.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from resource names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep recovery-control ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new recovery-control metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry routing control state,
  leave it out of the scanner contract.
- Add new relationship evidence only when the recovery-control API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or set routing control state, run the recovery-control data plane,
  or call any recovery-control configuration mutation API.
- Do not add the route53recoverycluster data-plane module to this package's
  dependency graph.
- Do not resolve recovery-control names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
