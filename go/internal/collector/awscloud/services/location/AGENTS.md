# AGENTS.md - internal/collector/awscloud/services/location guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Location Service domain types.
3. `scanner.go` - map, place index, tracker, geofence collection, and route
   calculator resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Location Service API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read device positions, position history, geofence geometries,
  place-search results, route calculations, or map tiles. Never call any
  `Create*`, `Update*`, `Delete*`, `Put*`, `Batch*`, `Associate*`,
  `Search*`, `Calculate*`, `GetDevicePosition*`, `GetGeofence`, `GetPlace`, or
  `GetMap*` API.
- Every node publishes its resource_id as the API-reported ARN (fallback to
  name). Source a resource's own edges on that ARN so they join the node.
- Emit the KMS edges only when AWS reports a key identifier. Set `target_arn`
  only when the identifier is ARN-shaped, matching the KMS scanner's published
  key resource_id.
- Emit the tracker-consumes-geofence-collection edge only when
  `ListTrackerConsumers` reports a consumer ARN. That ARN is the geofence
  collection ARN the collection node publishes, so key the target on it.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from resource names or AWS
  tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep Location Service resource ARNs, names, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Location Service metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a device position,
  geofence geometry, place result, or route, leave it out of the scanner
  contract.
- Add new relationship evidence only when the Location Service API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for KMS keys, the geofence collection ARN for
  consumer associations).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read device positions, geofence geometries, place-search results, route
  calculations, or map tiles, or call any Location Service mutation API.
- Do not resolve Location Service names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
