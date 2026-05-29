# AGENTS.md - internal/collector/awscloud/services/servicediscovery guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Cloud Map domain types.
3. `scanner.go` - resource and relationship emission orchestration.
4. `observations.go` - resource id and attribute mapping rules.
5. `relationships.go` - relationship target-type and join-key rules.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Cloud Map API access behind `Client`; do not import the AWS SDK into this
  package.
- NEVER call a Cloud Map mutation API (Create/Update/Delete for namespaces or
  services, RegisterInstance, DeregisterInstance,
  UpdateInstanceCustomHealthStatus, TagResource, UntagResource). The adapter
  `apiClient` interface excludes all of them and a reflection test asserts the
  exclusion.
- NEVER read or persist an instance attribute map. Those maps can hold
  caller-defined secrets. Record the instance COUNT only, from the Cloud Map
  service summary. The adapter never calls `ListInstances`, `GetInstance`,
  `GetInstancesHealthStatus`, `DiscoverInstances`, or
  `DiscoverInstancesRevision`; the reflection test asserts that exclusion too.
- The service resource id is `namespaceName/serviceName` with resource type
  `aws_cloud_map_service`. This matches the App Mesh
  virtual-node-to-Cloud-Map-service edge target exactly. Do not change either
  side without changing the other in lockstep.
- The namespace -> Route 53 hosted zone edge keys on `/hostedzone/<id>` to match
  the route53 scanner resource id. Cloud Map reports the bare zone id.
- Do NOT emit a namespace -> VPC edge. Cloud Map's read API does not report the
  VPC for a private DNS namespace; the VPC is reached transitively through the
  private Route 53 hosted zone. Inventing the edge is wrong graph truth.
- Every relationship sets a non-empty `target_type`.
- NEVER hardcode `arn:aws:`. The package keys resources by Cloud Map ids and the
  namespace/service identity, never by a synthesized partition ARN.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from resource names or tags.

## Common Changes

- Add a new Cloud Map metadata field by extending the relevant type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when Cloud Map reports both sides directly,
  and always set a non-empty `target_type` with a join key that matches the
  target scanner resource_id.
- Extend SDK pagination and the per-namespace service fan-out in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not read, list, discover, or persist instance attributes.
- Do not call any Cloud Map mutation API.
- Do not change the `namespaceName/serviceName` service resource id format
  (it is the App Mesh dangling-edge join key).
- Do not invent a namespace -> VPC edge.
- Do not add graph writes, reducer logic, or query behavior here.
- Do not add AWS credential loading or STS calls to this package.
