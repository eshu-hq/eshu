# AGENTS.md - internal/collector/awscloud/services/codeartifact guidance

## Read First

1. `README.md` - package purpose, exported surface, invariants, and evidence.
2. `types.go` - scanner-owned CodeArtifact domain types and `Client`.
3. `scanner.go` - domain and repository resource emission.
4. `relationships.go` - the four graph-join edges and their target identities.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - CodeArtifact slice
   requirements.

## Invariants

- Keep CodeArtifact API access behind `Client`; do not import the AWS SDK into
  this package.
- Metadata only. Never read, download, publish, copy, or delete a package
  version or asset. Keep `Client` limited to `List`/`Describe` reads; the
  reflection guard test fails the build if a payload or mutation method appears.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from domain or repository names.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant (or the documented
  `public_package_registry` allowlist entry) and a `target_resource_id` matching
  how the target scanner publishes its `resource_id`.
- Use API-reported ARNs directly so they stay partition-aware. If you ever
  synthesize an ARN, derive the partition through
  `awscloud.PartitionForBoundary`; never hardcode `arn:aws:`.
- Preserve stable domain and repository identities across repeated observations
  in the same AWS generation.
- Keep encryption-key ARNs, repository descriptions, and external-connection
  names out of metric labels.

## Common Changes

- Add a new CodeArtifact resource or edge by extending the scanner-owned type,
  writing a focused scanner test first (resource counts, every edge's
  `target_type` and `target_resource_id`, a `relguard.AssertObservations`
  call), then mapping it through `awscloud` envelope builders.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not resolve CodeArtifact upstream or external-connection references to
  concrete package or workload truth here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
