# AGENTS.md - internal/collector/awscloud/services/imagebuilder guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Image Builder domain types.
3. `scanner.go` - resource and relationship emission per resource type.
4. `observations.go` - resource-node attribute mapping.
5. `relationships.go` - relationship emission rules and join keys.
6. `helpers.go` - partition-aware ARN synthesis and scanner-side cloning helpers.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Image Builder API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist component build-document bodies, Dockerfile template
  bodies, instance user data, EC2 key pair names, scan findings, or build
  artifacts. The key pair is reduced to a configured boolean.
- Every resource node publishes its resource_id as the resource ARN. Source and
  target the pipeline/recipe/config edges on those exact ARNs.
- AWS reports the IAM instance profile and ECR repository by NAME and the S3
  logging bucket by NAME. Synthesize the partition-aware ARN the IAM, ECR, and
  S3 scanners publish with `awscloud.PartitionForBoundary`; never hardcode
  `arn:aws:`. GovCloud and China must resolve to the real node.
- Key subnet and security-group edges on the bare AWS id (subnet-..., sg-...)
  and leave `target_arn` empty.
- Emit the container-recipe-to-KMS-key edge only when AWS reports a key
  identifier. Set `target_arn` only when the identifier is ARN-shaped.
- Record the parent AMI of a recipe as an attribute, not an edge; Eshu has no
  EC2 AMI resource type.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from resource names or AWS
  tags.
- Keep Image Builder ARNs, names, versions, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Image Builder metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry a component body,
  Dockerfile body, user data, or a build artifact, leave it out of the contract.
- Add new relationship evidence only when the Image Builder API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality, the partition-aware synthesized ARN, or the
  bare id for subnets and security groups).
- Extend SDK pagination and per-resource get reads in the `awssdk` adapter, not
  here.

## What Not To Change Without An ADR

- Do not read component bodies, Dockerfile bodies, user data, scan findings, or
  build artifacts; do not call any Image Builder mutation or run-control API.
- Do not resolve Image Builder names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
