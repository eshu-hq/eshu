# AGENTS.md - internal/collector/awscloud/services/rolesanywhere guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Roles Anywhere domain types.
3. `scanner.go` - trust anchor, profile, and CRL resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Roles Anywhere API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist certificate private material, PEM certificate bundles,
  CRL body bytes, inline session policy documents, certificate attribute-mapping
  rule contents, or vended session credentials. Never call `GetCrl`,
  `GetSubject`, `ListSubjects`, or any `Create*`, `Update*`, `Delete*`,
  `Import*`, `Enable*`, or `Disable*` mutation API.
- Key the profile-to-IAM-role edge on the role ARN AWS reports, the resource_id
  the IAM scanner publishes. Drop duplicate and blank role ARNs.
- Emit the trust-anchor-to-ACM-PCA edge only for `AWS_ACM_PCA` trust anchors
  that report a CA ARN. Key it on that CA ARN, the resource_id the acmpca
  scanner publishes.
- Emit the CRL-to-trust-anchor edge only when AWS reports an associated
  trust-anchor ARN. Key it on that trust-anchor ARN, the resource_id the
  trust-anchor node publishes.
- Never synthesize an ARN. Forward reported ARNs verbatim so GovCloud and China
  partitions are preserved and never rewritten to a literal `arn:aws:`.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from trust anchor, profile,
  or CRL names, or AWS tags.
- Keep Roles Anywhere ARNs, names, and AWS error payloads out of metric labels.

## Common Changes

- Add a new Roles Anywhere metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry certificate material, a
  CRL body, a policy document, or credentials, leave it out of the contract or
  record only a boolean presence flag.
- Add new relationship evidence only when the Roles Anywhere API reports both
  sides directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for IAM roles and ACM PCA certificate
  authorities, the trust-anchor ARN for the parent trust anchor).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read CRL bodies, certificate material, subjects, or credentials, run
  any mutation, or call any Roles Anywhere mutation API.
- Do not resolve Roles Anywhere names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
