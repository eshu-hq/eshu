# AGENTS.md - internal/collector/awscloud/services/licensemanager guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned License Manager domain types.
3. `scanner.go` - license-configuration resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, instance-id extraction, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep License Manager API access behind `Client`; do not import the AWS SDK
  into this package.
- Never grant, check out, or mutate a license; never read a license entitlement
  token; never read license usage records. Never call `GetLicense`,
  `CheckoutLicense`, `CheckInLicense`, `GetAccessToken`, `CreateGrant`, or any
  `Create*`, `Update*`, `Delete*` mutation API. The adapter `apiClient`
  interface lists only the three List read operations and the exclusion test
  fails the build if a checkout or mutation method is ever added.
- The configuration node publishes its resource_id as the configuration ARN
  (fallback to id, then name). Source the configuration's edges on that exact
  value so they join the configuration node.
- Emit the configuration-applies-to-instance edge only for an `EC2_INSTANCE`
  association whose `ResourceArn` yields a bare instance id (`i-...`). Key the
  edge on that bare id, the convention for an EC2 instance target. Read the bare
  id from the ARN; never synthesize an ARN.
- Do not emit an edge for `EC2_HOST`, `EC2_AMI`, `RDS`, or
  `SYSTEMS_MANAGER_MANAGED_INSTANCE` associations: there is no resolvable target
  node, so an edge would dangle. Record them only as configuration metadata.
- Every relationship sets a non-empty `target_type` that is a declared
  `awscloud.ResourceType*` constant or a documented
  `relguard.KnownTargetTypeAllowlist` value, and a `target_resource_id` matching
  how the target node is keyed.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from configuration names or
  AWS tags.
- Keep License Manager ARNs, names, counts, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new License Manager metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry an entitlement token or
  usage measurement, leave it out of the scanner contract.
- Add new relationship evidence only when the License Manager API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (the bare `i-...` id for an EC2 instance).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read entitlement tokens, check out or check in licenses, create or
  revoke grants, or call any License Manager mutation API.
- Do not key an edge to `EC2_HOST`, `EC2_AMI`, `RDS`, or SSM-managed-instance
  associations without first publishing or allowlisting a resolvable target
  resource type.
- Do not resolve License Manager names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
