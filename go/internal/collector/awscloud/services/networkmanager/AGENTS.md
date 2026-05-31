# AGENTS.md - internal/collector/awscloud/services/networkmanager guidance

## Read First

1. `README.md` - package purpose, global-service region behavior, exported
   surface, resource_id shapes, and invariants.
2. `types.go` - scanner-owned Network Manager domain types.
3. `observations.go` - resource observation builders and resource_id derivation.
4. `relationships.go` - relationship emission rules and join keys.
5. `scanner.go` - global-network and core-network resource/relationship
   emission.
6. `helpers.go` - partition-aware Network Manager ARN synthesis, transit gateway
   id extraction, and cloning helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Network Manager API access behind `Client`; do not import the AWS SDK
  into this package.
- Network Manager is global: the `awssdk` adapter pins the partition's
  control-plane region. Never assume the claim region reaches the control plane.
- Never read route analyses, network routes, network telemetry, or routing
  policy documents. Never call any Create/Update/Delete, Register/Deregister,
  Associate/Disassociate, Tag, Put, or Start mutation API.
- Every node publishes its API-reported ARN as resource_id. Network Manager ARNs
  have an empty region segment.
- Child records report only the parent id. Synthesize the partition-aware parent
  ARN with `awscloud.PartitionForBoundary`; never hardcode `arn:aws:` - GovCloud
  and China must resolve to the real parent node.
- Key the transit gateway registration edge on the **bare** `tgw-` id (the
  resource_id the transit gateway scanner publishes), extracted from the reported
  transit gateway ARN. Leave `target_arn` empty for that edge.
- Do not key a device-to-subnet edge: Eshu does not yet publish a VPC subnet
  resource node. Keep `subnet_arn` as context metadata only.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or environment truth from names, locations, or AWS tags.

## Common Changes

- Add a new Network Manager metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. Leave route/telemetry/policy payloads out.
- Add new relationship evidence only when the Network Manager API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read route analyses, network routes, telemetry, or routing policy, or
  call any Network Manager mutation API.
- Do not resolve Network Manager names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
