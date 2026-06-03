# AGENTS.md - internal/collector/awscloud/services/s3 guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned S3 domain types.
3. `scanner.go` - bucket and logging-target relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep S3 API access behind `Client`; do not import the AWS SDK into this
  package.
- Never read objects, list object keys, or mutate S3 buckets.
- The scanner MAY emit the normalized, derived `aws_resource_policy_permission`
  fact from the bucket policy: per statement, the effect, normalized
  action/resource patterns, condition key/operator NAMES, and derived grantee facts
  (principal account ids, principal ARNs, principal types, public, cross-account)
  — the resource-side analog of `aws_iam_permission` (PR4b of #1134,
  owner-approved). The `awssdk` adapter parses the policy transiently and hands
  this package only the derived `Bucket.ResourcePolicyStatements`; this package
  never sees the raw policy document.
- Never persist the raw bucket policy JSON, policy statement bodies, statement
  Sids, policy CONDITION VALUES, ACL grants, replication rules, lifecycle rules,
  notification configuration, inventory configuration, analytics configuration,
  or metrics configuration. Normalized/derived policy actions, resources, and
  condition key/operator NAMES are allowed via `aws_resource_policy_permission`; raw
  statement bodies and condition values are not.
- Never persist website index or error document object keys.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from bucket names or tags.
- Preserve stable bucket identities across repeated observations in the same
  AWS generation.
- Keep bucket ARNs, bucket names, tags, prefixes, and KMS key IDs out of metric
  labels.

## Common Changes

- Add a new S3 metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Extend the derived `s3_bucket_posture` fact by adding a derived boolean or
  safe identifier to `awscloud.S3BucketPostureObservation` and the
  `bucketPostureObservation` mapper, test-first. The posture fact carries only
  derived booleans and safe identifiers/ARNs. The policy-derived booleans
  (`PolicyGrantsPublic`, `PolicyGrantsCrossAccount`) arrive already derived on
  the `Bucket` model; this package never sees the raw policy document. PR1 is
  facts-only: do not add a graph edge or reducer projection for posture here.
- Extend `s3_external_principal_grant` only with bounded principal metadata
  already derived on `Bucket.ExternalPrincipalGrants`. Public, cross-account,
  AWS service, and unsupported-principal outcomes are reported evidence only.
  Unsupported principal facts keep the principal type key, not the raw
  identifier.
- Extend `aws_resource_policy_permission` only with normalized/derived statement
  metadata already derived on `Bucket.ResourcePolicyStatements` (effect,
  normalized actions/resources, condition key/operator NAMES, principal account ids /
  ARNs / types, public / cross-account). The derivation lives in the `awssdk`
  adapter (`deriveBucketPolicyResourcePermissionStatements`); never add raw
  statement bodies or condition values to the statement or the fact. This fact is
  the facts foundation for resource-policy-aware CAN_PERFORM (a later reducer
  follow-up); do not add a graph edge or reducer projection here.
- Add new relationship evidence only when S3 reports both sides directly and
  the target identity is not sensitive.
- Extend SDK pagination and optional-not-configured handling in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not read objects, list object versions, list multipart uploads, mutate
  buckets, or mutate bucket configuration.
- Do not resolve bucket names, tags, website status, or logging targets into
  workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
