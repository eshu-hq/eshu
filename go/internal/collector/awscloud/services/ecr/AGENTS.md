# AGENTS.md - internal/collector/awscloud/services/ecr guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned ECR domain types.
3. `scanner.go` - repository, lifecycle policy, and image-reference emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage and metadata-only data boundaries.

## Invariants

- Keep ECR API access behind `Client`; do not import the AWS SDK into this
  package.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from repository names or tags.
- Preserve stable repository, lifecycle policy, and image-reference identities
  across repeated observations in the same AWS generation.
- Keep lifecycle policy JSON, repository ARNs, tags, and image digests out of
  metric labels.

## Common Changes

- Add a new ECR resource by extending the scanner-owned type, writing a focused
  scanner test first, then mapping it through `awscloud` envelope builders.
- Add image-reference fields only when the ECR API reports them directly or the
  mapping is documented in current public docs.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without Architecture-Owner Approval

- Do not resolve ECR image references to workloads here; correlation belongs in
  reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
