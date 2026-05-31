# AGENTS.md - internal/collector/awscloud/services/securitylake guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Security Lake domain types.
3. `scanner.go` - data lake, log source, and subscriber resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Security Lake API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read ingested security log records or object contents. Never persist the
  subscriber external id (a trust-establishment credential) or the subscriber
  endpoint (a private notification destination). Never call any `Create*`,
  `Update*`, `Delete*`, `Register*`, or other mutation API.
- The data lake node publishes its resource_id as the data lake ARN (fallback to
  `securitylake:<region>`). Key the log-source-in-data-lake edge on that exact
  value so it joins the data lake node.
- Source a subscriber's own edges on the subscriber ARN (fallback to id), the
  resource_id the subscriber node publishes.
- Use the bucket ARN AWS reports directly for S3 edges; it is already
  partition-correct, so never synthesize `arn:aws:`. The same ARN keys the
  data-lake-to-Lake-Formation registered-resource edge.
- Emit the data-lake-to-KMS edge only when AWS reports a customer key
  identifier; skip the `S3_MANAGED` and `AWS_OWNED_KMS_KEY` sentinels. Set
  `target_arn` only when the identifier is ARN-shaped.
- Emit IAM-role edges only for ARN-shaped role identifiers, targeting
  `aws_iam_role` keyed by the role ARN.
- Skip the data-lake-to-Glue edge: Security Lake does not report a resolvable
  Glue identifier, so do not dangle one.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from data lake, source, or
  subscriber names, or AWS tags.

## Common Changes

- Add a new Security Lake metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry record content, a
  credential, an external id, or an endpoint, leave it out of the contract.
- Add new relationship evidence only when the Security Lake API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read ingested records, read subscriber credentials, or call any
  Security Lake mutation API.
- Do not resolve Security Lake names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
