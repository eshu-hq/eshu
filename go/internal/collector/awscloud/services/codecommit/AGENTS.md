# AGENTS.md - internal/collector/awscloud/services/codecommit guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CodeCommit domain types.
3. `scanner.go` - repository resource and relationship emission.
4. `relationships.go` - KMS-key and SNS-topic edge construction.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud-scanners.md` - CodeCommit row.

## Invariants

- Metadata only. Never read commits, refs, blobs, file contents, pull-request
  bodies, or comment text. Keep CodeCommit API access behind `Client`; do not
  import the AWS SDK into this package. The `awssdk` adapter's read surface
  excludes every content reader and mutation by construction; keep its
  exclusion reflection guard green.
- Clone-URL evidence is host-only in resource attributes. The full clone URLs
  are correlation anchors, not attributes, so a clone URL's path or userinfo
  never persists as a field.
- Preserve the repository as a code-to-cloud anchor: publish the repository
  name and clone URLs as correlation anchors so CodeBuild / CodePipeline Git
  sources and Amplify apps join the repository node.
- Every relationship sets a declared `awscloud.ResourceType*` target type and a
  `target_resource_id` that matches how the target scanner publishes its
  `resource_id`: the KMS edge keys on the bare key id when AWS reports one (KMS
  scanner `resource_id`), or the key ARN when ARN-shaped; the SNS edge keys on
  the topic ARN.
- Do not synthesize ARNs. Every ARN on a CodeCommit fact (repository, KMS key,
  SNS topic) comes from the API, so the partition is already correct; never
  hardcode `arn:aws:`.
- Emit reported evidence only. Do not infer deployment, workload, or
  deployable-unit truth from repository names or tags.

## Common Changes

- Add a new CodeCommit metadata field by extending the scanner-owned type,
  writing a focused scanner test first, then mapping it through `awscloud`
  envelope builders.
- Add a relationship only when CodeCommit reports an ARN-addressable or
  bare-id-addressable target that names a declared resource family, with a
  `relguard.AssertObservations` test for the new edge.
- Extend SDK pagination or batch chunking in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add commit, ref, blob, file-content, pull-request, or comment reads.
- Do not resolve repository-to-CI correlation here; that belongs in reducers,
  driven by the correlation anchors this scanner publishes.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
