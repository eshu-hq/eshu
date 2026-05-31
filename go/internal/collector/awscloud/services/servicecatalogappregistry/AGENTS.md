# AGENTS.md - internal/collector/awscloud/services/servicecatalogappregistry guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned AppRegistry domain types.
3. `scanner.go` - application and attribute-group resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppRegistry API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist the attribute-group content body (the
  application-metadata JSON document) or associated-resource tag values. Never
  call any `Get*`/`Describe*` content read or any `Create*`, `Update*`,
  `Delete*`, `Associate*`, `Disassociate*`, `Put*`, or `Tag*` mutation API.
- The application node publishes its resource_id as the application ARN
  (fallback to id). Source the application's own edges on that exact value.
- The attribute-group node publishes its resource_id as the group ARN (fallback
  to id). Key the application-to-attribute-group edge on that value so it joins
  the group node.
- Emit the application-to-CloudFormation-stack edge only for CFN_STACK
  associated resources whose reported ARN is a CloudFormation stack ARN. Key it
  by that stack ARN, matching the `cloudformation` scanner's published stack
  resource_id. Skip RESOURCE_TAG_VALUE and any non-stack association rather than
  dangling an edge.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from application, attribute
  group, or stack names, or AWS tags.
- Preserve stable application and attribute-group identities across repeated
  observations in the same AWS generation.
- Keep AppRegistry ARNs, names, descriptions, tags, and AWS error payloads out
  of metric labels.

No-Regression Evidence: metadata-only control-plane scanner; new read path, no
change to existing hot paths. `go test ./internal/collector/awscloud/services/servicecatalogappregistry/...`
green.

No-Observability-Change: reuses shared AWS pagination span + API-call/throttle
counters; no telemetry contract change.

## Common Changes

- Add a new AppRegistry metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a content body or tag
  value, leave it out of the scanner contract.
- Add new relationship evidence only when the AppRegistry API reports both
  sides directly and the target identity matches an existing scanner's
  published resource_id shape (ARN-equality for CloudFormation stacks and
  attribute groups).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read attribute-group content bodies, read associated-resource tag
  values, or call any AppRegistry mutation API.
- Do not resolve AppRegistry names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
