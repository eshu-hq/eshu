# AGENTS.md - internal/collector/awscloud/services/codeguru guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned CodeGuru Reviewer/Profiler domain types.
3. `scanner.go` - association and profiling-group resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware CodeCommit ARN
   synthesis, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep CodeGuru API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read code-review findings, recommendation content, analyzed source,
  profiling samples, aggregated profiles, flame graphs, recommendation reports,
  or agent telemetry. Never call any `Create*`, `Update*`, `Delete*`,
  `Associate*`, `Disassociate*`, `PutPermission*`, or `Configure*` mutation API,
  and never call `GetProfile`, `GetRecommendations`, `ListRecommendations`,
  `DescribeCodeReview`, `ListCodeReviews`, `ListFindingsReports`, or
  `BatchGetFrameMetricData`.
- One `service_kind` (`codeguru`) covers both Reviewer and Profiler. The Scan
  switch trims whitespace and writes the canonical value back; never add an
  empty-bodied non-default case.
- The association node publishes its resource_id as the association ARN (fallback
  to association id, then name). The profiling-group node publishes its ARN
  (fallback to name). Source each node's own edges on that same value.
- Emit the association-to-CodeCommit-repository edge only when the provider type
  is CodeCommit. CodeGuru reports only the repo name and owning account, so
  synthesize the CodeCommit repository ARN
  (`arn:<partition>:codecommit:<region>:<owner>:<name>`) with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:`; GovCloud and
  China must resolve to the real repository node. Skip the edge when the owner
  account or region is missing rather than dangling it.
- Non-CodeCommit providers and the CodeStar connection ARN / S3 bucket name are
  resource attributes only, never edges - they would dangle to unscanned
  third-party endpoints.
- The profiling-group compute platform is a resource attribute; CodeGuru reports
  no structured compute resource identifier to key an edge on.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from names or AWS tags.
- Keep CodeGuru ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new CodeGuru metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry findings, recommendation, profiling
  sample, or source content, leave it out of the scanner contract.
- Add new relationship evidence only when the CodeGuru API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read findings, recommendations, code reviews, profiles, or frame
  metrics, or call any CodeGuru mutation API.
- Do not promote a non-CodeCommit provider reference to a graph edge.
- Do not resolve CodeGuru names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
