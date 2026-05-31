# AGENTS.md - internal/collector/awscloud/services/dax guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned DAX domain types.
3. `scanner.go` - cluster, subnet group, and parameter group fact emission.
4. `relationships.go` - cluster and subnet group relationship emission.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep DAX API access behind `Client`; do not import the AWS SDK into this
  package.
- Never call CreateCluster, DeleteCluster, UpdateCluster,
  IncreaseReplicationFactor, DecreaseReplicationFactor, RebootNode,
  CreateSubnetGroup, DeleteSubnetGroup, UpdateSubnetGroup, CreateParameterGroup,
  DeleteParameterGroup, UpdateParameterGroup, TagResource, UntagResource, or any
  mutation/data API.
- Never read or persist cached DynamoDB item data, query results, or node
  endpoint payloads. The discovery endpoint address is plain connection metadata.
- DAX does not report a server-side-encryption KMS key ARN. Record only the SSE
  status; never synthesize a `kms_key_id`, `kms_key_arn`, or a cluster-to-KMS
  edge, and never invent a cluster-to-DynamoDB-table edge.
- DAX subnet groups and parameter groups have no ARN; key both by name. Parameter
  group facts carry name and description only - never call DescribeParameters.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from cluster names or tags.
- Preserve stable identities (cluster ARN preferred, then cluster name; subnet
  and parameter groups by name) across repeated observations in the same AWS
  generation.
- Keep cluster names, subnet/parameter group names, endpoint addresses, tags,
  and ARNs out of metric labels.
- Edge targets must match how the publishing scanner keys its resource_id: subnet
  group by name, bare vpc-/subnet-/sg- ids for EC2 targets, and the IAM role ARN.

## Common Changes

- Add a new DAX metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. Reject additions that would expose cached item data, query
  results, node endpoint payloads, or parameter values.
- Add new relationship evidence only when the DAX API reports both sides directly
  and the target identity is not sensitive.
- Extend SDK pagination or tag reads in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not create, delete, or modify clusters, subnet groups, or parameter groups.
- Do not resolve cluster names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not widen `Cluster`, `SubnetGroup`, or `ParameterGroup` beyond the fields
  listed in `README.md`.
