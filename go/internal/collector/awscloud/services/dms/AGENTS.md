# AGENTS.md - internal/collector/awscloud/services/dms guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned DMS domain types.
3. `scanner.go` - instance, subnet group, endpoint, and task resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DMS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read migrated rows, endpoint connection credentials, passwords, server
  names used as credentials, connection attributes, SSL key material, external
  table definitions, task settings, or table-mapping bodies. Never call any
  mutation, `Start*`, `Stop*`, `TestConnection`, `RefreshSchemas`, or
  `Reload*` API.
- The replication instance node publishes its resource_id as the instance ARN
  (fallback to identifier). Key the task runs-on-instance edge on the instance
  ARN the task reports.
- The endpoint node publishes its resource_id as the endpoint ARN (fallback to
  identifier). Key the task source/target endpoint edges on the endpoint ARN
  the task reports.
- The subnet group node publishes its resource_id as the subnet-group
  identifier (no subnet-group ARN exists). Key the instance in-subnet-group edge
  on that identifier.
- Key instance-to-subnet, instance-to-security-group, subnet-group-to-VPC, and
  subnet-group-to-subnet edges on the bare AWS id the EC2 scanner publishes.
- Emit instance-to-KMS and endpoint-to-KMS edges only when AWS reports a key
  identifier. Set `target_arn` only when the identifier is ARN-shaped, matching
  the KMS scanner's published key resource_id.
- Emit the endpoint-to-S3 edge only when an S3 endpoint reports a bucket name.
  DMS reports a bucket NAME, so synthesize the bucket ARN with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud and
  China must resolve to the real bucket node.
- Emit the endpoint-to-Kinesis and endpoint-to-secret edges only when DMS
  reports a resolvable stream ARN or secret reference. Do not synthesize RDS or
  Redshift cluster targets from a server hostname or S3 staging config; those do
  not resolve to the target scanner's published resource_id.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from instance, endpoint,
  task, or subnet-group names, or AWS tags.
- Preserve stable instance, subnet group, endpoint, and task identities across
  repeated observations in the same AWS generation.
- Keep DMS resource ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new DMS metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry a credential, password, server name,
  connection attribute, SSL key, external table definition, task setting, or
  table-mapping body, leave it out of the scanner contract.
- Add new relationship evidence only when the DMS API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for endpoints, instances, KMS keys, Kinesis
  streams, and secrets; the synthesized partition-aware ARN for S3 buckets; the
  bare AWS id for EC2 subnets, security groups, and VPCs; the subnet-group
  identifier for the DMS subnet-group node).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read migrated rows, test connections, refresh schemas, reload tables,
  start/stop tasks, or call any DMS mutation API.
- Do not persist endpoint credentials, passwords, server names, connection
  attributes, SSL key material, external table definitions, task settings, or
  table-mapping bodies.
- Do not resolve DMS names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
