# AGENTS.md - internal/collector/awscloud/services/timestream guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Timestream domain types.
3. `scanner.go` - database and table resource and relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Timestream API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read time-series records, measure values, or query results. Never call
  `WriteRecords`, any `Query` (the timestream-query module is never imported),
  `BatchLoad*`, or any `Create*`, `Update*`, `Delete*` mutation API.
- The database node publishes its resource_id as the database ARN (fallback to
  name). Key the table-in-database edge on that exact value so it joins the
  database node.
- Source a table's own edges on the table ARN, the resource_id the table node
  publishes.
- Emit the database-to-KMS-key edge only when AWS reports a key identifier. Set
  `target_arn` only when the identifier is ARN-shaped, matching the KMS
  scanner's published key resource_id.
- Emit the table-to-S3 edge only when a magnetic-store rejected-data bucket is
  configured. Timestream reports a bucket NAME, so synthesize the bucket ARN
  with `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud
  and China must resolve to the real bucket node.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from database, table, or
  bucket names, or AWS tags.
- Preserve stable database and table identities across repeated observations in
  the same AWS generation.
- Keep Timestream resource ARNs, names, retention, tags, and AWS error payloads
  out of metric labels.

## Common Changes

- Add a new Timestream metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry record or measure
  values, leave it out of the scanner contract.
- Add new relationship evidence only when the Timestream API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for KMS keys and S3 buckets, the database ARN
  for the parent database).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read records or measures, run queries, write records, run batch loads,
  mutate databases, mutate tables, or call any Timestream mutation API.
- Do not add the timestream-query module to this package's dependency graph.
- Do not resolve Timestream names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
