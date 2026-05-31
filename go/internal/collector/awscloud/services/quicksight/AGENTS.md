# AGENTS.md - internal/collector/awscloud/services/quicksight guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned QuickSight domain types.
3. `scanner.go` - data source, dataset, dashboard, and analysis resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   VPC connection id extraction, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep QuickSight API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist data-source credentials, connection passwords, secret
  connection parameters, the Secrets Manager secret value, custom-SQL query
  bodies, dataset column data, row-level security values, or visual definitions.
  Only a boolean `secret_configured` flag records that a secret exists.
- A not-subscribed account returns an empty snapshot; the adapter, not the
  scanner, maps that case. Genuine authorization failures must still fail.
- The data source node publishes its resource_id as the data source ARN
  (fallback to id). Key dataset-to-data-source edges on the data source ARN.
- Emit a backing-store edge only when the connector reports a resolvable target
  id: bare Redshift cluster id, bare RDS DB instance id, bare Athena workgroup
  name, or the partition-aware synthesized S3 bucket ARN. Never hardcode
  `arn:aws:`; synthesize with `awscloud.PartitionForBoundary` so GovCloud and
  China resolve to the real bucket node.
- Emit VPC-connection security-group and subnet edges only when the data source
  uses a VPC connection that resolved to a known summary. Key both by bare id.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from resource names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation.
- Keep QuickSight resource ARNs, names, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new QuickSight metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry credential, secret, SQL,
  or visual-definition content, leave it out of the scanner contract.
- Add new relationship evidence only when the QuickSight API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination and describe fan-out in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read credentials, secrets, SQL bodies, or visual definitions, and do
  not call any QuickSight mutation, ingestion, embed, or permissions API.
- Do not resolve QuickSight names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
