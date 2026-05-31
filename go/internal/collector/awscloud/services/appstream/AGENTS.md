# AGENTS.md - internal/collector/awscloud/services/appstream guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, edge
   conventions, and invariants.
2. `types.go` - scanner-owned AppStream domain types.
3. `scanner.go` - fleet, stack, image builder, and image resource and
   relationship emission, plus the name-to-resource_id association indexes.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation, partition-aware bucket ARN synthesis,
   and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep AppStream API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read streaming sessions, user data, or session scripts. Never call
  `CreateStreamingURL`, any `Describe`Sessions/Users API, or any `Create*`,
  `Update*`, `Delete*`, `Start*`, `Stop*` mutation API.
- Every node publishes its resource_id as its ARN (fallback to name). Key fleet,
  image-builder, and stack edges on those exact values.
- The fleet-to-stack association API reports the stack by NAME; resolve it to the
  stack node resource_id (its ARN) via the stack name index before keying the
  edge, so the edge joins the stack node instead of a dangling name.
- VPC subnet/security-group edges key on the bare AWS ids (`subnet-...`,
  `sg-...`) the EC2 scanner publishes, not ARNs.
- IAM role and image edges key on the ARNs AppStream reports. Set `target_arn`
  on those edges.
- Stack S3 edges synthesize the partition-aware bucket ARN with
  `awscloud.PartitionForBoundary` and never hardcode `arn:aws:` - GovCloud and
  China must resolve to the real bucket node. Only the application-settings
  bucket and HOMEFOLDERS storage-connector buckets are S3 buckets; Google Drive
  and OneDrive connectors carry domains, not buckets.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Scope image reads to PRIVATE and SHARED visibility; do not scan the
  AWS-managed PUBLIC base-image catalog.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from fleet, stack, image, or
  bucket names, or AWS tags.

## Common Changes

- Add a new AppStream metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry session, user, or
  credential content, leave it out of the scanner contract.
- Add new relationship evidence only when the AppStream API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (bare ids for subnets/security groups, ARNs for IAM roles
  and the synthesized S3 bucket, the stack/image node id for internal edges).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read sessions, users, or session scripts, mint streaming URLs, or call
  any AppStream mutation API.
- Do not resolve AppStream names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
