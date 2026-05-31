# AGENTS.md - internal/collector/awscloud/services/cloudhsmv2 guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned CloudHSM v2 domain types.
3. `scanner.go` - cluster and backup resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, certificate-presence detection, and
   scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep CloudHSM v2 API access behind `Client`; do not import the AWS SDK into
  this package.
- NEVER read or persist cryptographic key material, certificate PEM bodies, the
  cluster certificate signing request body, or the cluster's Pre-Crypto Officer
  password. Certificate state is a boolean presence flag per field, never the
  body.
- CloudHSM v2 clusters have no API ARN. The cluster node publishes the bare
  cluster id as its resource_id; key the backup-of-cluster edge on that exact
  value so it joins the cluster node.
- The backup node publishes the bare backup id; the backup ARN is recorded but
  is not the join key.
- Key the cluster-in-VPC, cluster-in-subnet, and cluster-uses-security-group
  edges by the bare AWS id (`vpc-…`, `subnet-…`, `sg-…`), the resource_id the
  EC2 scanner publishes. Never synthesize an ARN for these edges.
- De-duplicate subnet edges across availability zones.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from cluster, backup, or tag
  values.
- Keep CloudHSM v2 ARNs, ids, certificate state, tags, and AWS error payloads
  out of metric labels.

## Common Changes

- Add a new CloudHSM v2 metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry key material, a
  certificate body, a CSR body, or the PRECO password, leave it out of the
  scanner contract.
- Add new relationship evidence only when the CloudHSM v2 API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (bare AWS id for VPC/subnet/security group, bare cluster id
  for the parent cluster).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read or persist key material, certificate bodies, CSR bodies, or the
  Pre-Crypto Officer password.
- Do not call InitializeCluster, any mutation API, or any resource-policy read.
- Do not resolve CloudHSM v2 names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
