# AGENTS.md - internal/collector/awscloud/services/datazone guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned DataZone domain types.
3. `scanner.go` - domain, project, environment, and data source resource and
   relationship emission.
4. `observations.go` - resource observation builders for each resource family.
5. `relationships.go` - relationship emission rules and join keys.
6. `helpers.go` - resource_id derivation, partition-aware Redshift cluster ARN
   synthesis, and scanner-side cloning helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DataZone API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read or persist business glossaries, glossary terms, catalog asset
  content, listings, subscription data, time-series data, lineage, relational
  filter expressions, or access credentials. Never call any `Create*`,
  `Update*`, `Delete*`, `Accept*`, `Reject*`, or other mutation API.
- The domain node publishes its resource_id as the DataZone domain id (fallback
  to the domain ARN). Key every child-in-domain edge on that exact value so it
  joins the domain node.
- Projects, environments, and data sources publish their resource_id as the
  DataZone id of the resource. Source each child's own edges on that id.
- Emit the domain-to-KMS-key edge only when DataZone reports a key identifier.
  Set `target_arn` only when the identifier is ARN-shaped, matching the KMS
  scanner's published key resource_id.
- Emit a domain-to-IAM-role edge only for a genuine IAM role ARN
  (`arn:<partition>:iam::<account>:role/...`), matching the IAM scanner's
  published role resource_id. A non-role principal yields no edge.
- Emit the data-source-to-Glue-database edge keyed by the Glue database name (the
  Glue scanner's published database resource_id). Emit the
  data-source-to-Redshift-cluster edge keyed by the partition-aware cluster ARN
  synthesized with `awscloud.PartitionForRegion` / `PartitionForBoundary` and
  never hardcode `arn:aws:`. Do not edge Redshift Serverless workgroups: their
  published ARN cannot be synthesized from the workgroup name, so skip rather
  than dangle.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from domain, project, or data
  source names, or AWS tags.
- Keep DataZone ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new DataZone metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry glossary, asset, subscription, or
  credential content, leave it out of the scanner contract.
- Add new relationship evidence only when the DataZone API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (the domain id for the parent domain, the Glue database name,
  the synthesized Redshift cluster ARN, the IAM role ARN, the KMS key identifier).
- Extend SDK pagination and GetDomain/GetDataSource enrichment in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not read glossaries, glossary terms, assets, asset content, listings,
  subscriptions, time-series data, or lineage, and do not call any DataZone
  mutation API.
- Do not resolve DataZone names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
