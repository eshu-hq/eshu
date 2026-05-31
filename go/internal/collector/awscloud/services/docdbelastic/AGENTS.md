# AGENTS.md - internal/collector/awscloud/services/docdbelastic guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned DocumentDB Elastic domain types.
3. `scanner.go` - cluster resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DocumentDB Elastic API access behind `Client`; do not import the AWS SDK
  into this package.
- Never read document contents, collections, indexes, or query results. Never
  read or persist the admin password, the admin user name, or the cluster
  endpoint connection string. Never call any `Create*`, `Update*`, `Delete*`,
  `Copy*`, `Restore*`, `Apply*`, `Start*`, or `Stop*` mutation API.
- The cluster node publishes its resource_id as the cluster ARN (fallback to
  name). Source every outgoing edge on that exact value.
- Emit cluster-to-subnet and cluster-to-security-group edges keyed by the bare
  `subnet-...` and `sg-...` ids AWS reports - these match the EC2 scanner's
  published subnet and security-group resource_ids. Do not synthesize an ARN.
- Emit the cluster-to-KMS-key edge only when AWS reports a key identifier. Set
  `target_arn` only when the identifier is ARN-shaped, matching the KMS
  scanner's published key resource_id.
- Emit the cluster-to-admin-secret edge only for `SECRET_ARN` auth, when AWS
  reports an ARN-shaped secret reference. It matches the Secrets Manager
  scanner's published secret resource_id. Never read the secret value. Drop the
  admin user name entirely under `PLAIN_TEXT` auth.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from cluster names or AWS
  tags.
- Preserve stable cluster identities across repeated observations in the same
  AWS generation.
- Keep cluster ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new DocumentDB Elastic metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry document, credential,
  endpoint, or password content, leave it out of the scanner contract.
- Add new relationship evidence only when the DocumentDB Elastic API reports
  both sides directly and the target identity matches an existing scanner's
  published resource_id shape (bare id for subnet/security-group, key id/ARN for
  KMS, secret ARN for Secrets Manager).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read documents, collections, indexes, or query results, read the admin
  password or user name, read the cluster endpoint connection string, or call
  any DocumentDB Elastic mutation API.
- Do not resolve cluster names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not merge this scanner with the classic `docdb` scanner; they are distinct
  services with separate APIs, resource types, and service kinds.
