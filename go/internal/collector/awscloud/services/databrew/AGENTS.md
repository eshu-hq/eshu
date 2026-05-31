# AGENTS.md - internal/collector/awscloud/services/databrew guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned DataBrew domain types.
3. `scanner.go` - dataset, recipe, job, and project resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   Glue table id synthesis, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataBrew API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read or persist recipe step expressions, transformation operations or
  their parameters, custom SQL query strings, dataset path-option parameter
  values, or sample data. Never call any `Create*`, `Update*`, `Delete*`,
  `Start*`, `Stop*`, `Publish*`, or `Send*` mutation API, and never call a
  `Describe*` detail read that would expose step expressions or sample data.
  Record only the recipe step COUNT.
- The dataset node publishes its resource_id as the dataset NAME and the recipe
  node as the recipe NAME, because jobs and projects reference them by name. Key
  the job-to-dataset, project-to-dataset, and project-to-recipe edges on those
  exact names so they join the internal nodes.
- The job and project nodes publish their resource_id as their ARN (fallback to
  name).
- Emit the dataset-to-S3 and job-to-S3 edges only when an S3 bucket is
  configured. DataBrew reports a bucket NAME, so synthesize the bucket ARN with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud and
  China must resolve to the real bucket node.
- Emit the dataset-to-Glue-table edge only when the dataset reads a Data Catalog
  table, keyed by the `<database>/<table>` identity the Glue table scanner
  publishes.
- Do NOT emit a dataset-to-Redshift-cluster edge. DataBrew reports only a Glue
  connection name and table name for a database (Redshift/JDBC) input, never a
  cluster ARN or identifier, so an edge would dangle. Skip it and record the
  connection name as metadata.
- Emit the IAM-role edges only when AWS reports a role ARN. Set `target_arn`
  only when the identifier is ARN-shaped, matching the IAM scanner's published
  role resource_id.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from DataBrew names or AWS
  tags.
- Preserve stable dataset, recipe, job, and project identities across repeated
  observations in the same AWS generation.
- Keep DataBrew resource ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new DataBrew metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry a step expression, SQL string,
  parameter value, or sample data, leave it out of the scanner contract.
- Add new relationship evidence only when the DataBrew API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for IAM roles and S3 buckets, `<db>/<table>`
  for Glue tables, the resource name for internal DataBrew nodes).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read recipe steps, dataset path-option values, custom SQL, or sample
  data; do not call any DataBrew Describe or mutation API.
- Do not edge a database (Redshift/JDBC) dataset input to a Redshift cluster
  without a reported cluster identity.
- Do not resolve DataBrew names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
