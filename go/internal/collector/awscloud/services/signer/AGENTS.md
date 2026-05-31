# AGENTS.md - internal/collector/awscloud/services/signer guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Signer domain types.
3. `scanner.go` - signing-profile and signing-platform resource and relationship
   emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Signer API access behind `Client`; do not import the AWS SDK into this
  package.
- Never start a signing job, read signing material private keys, read
  signed-object payloads, or call any `Start*`, `Sign*`, `Put*`, `Cancel*`,
  `Revoke*`, `Add*Permission`, `Remove*Permission`, `Tag*`, or `Untag*` API.
  Never read signing jobs (`ListSigningJobs`, `DescribeSigningJob`) or the
  revocation status.
- Persist signing-parameter NAMES only; never persist signing-parameter values,
  which can carry user-supplied data.
- The signing-profile node publishes its resource_id as the profile ARN
  (fallback to name). Source the profile's own edges on that exact value so they
  join the profile node.
- The signing-platform node publishes its resource_id as the bare platform id.
  Signer platforms carry no ARN; never synthesize one.
- Emit the profile-to-ACM-certificate edge only when AWS reports an ARN-shaped
  signing material certificate ARN. Set `target_arn` to that ARN; skip the edge
  for a non-ARN identifier (keep the raw value on the node only).
- Emit the profile-to-signing-platform edge keyed by the bare platform id, with
  no `target_arn`, targeting `aws_signer_signing_platform`.
- Do not emit S3, KMS, or Lambda edges from a signing profile: profiles do not
  report those dependencies. An S3 source/destination is reported only on a
  signing job, which is data-plane and never read.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from profile, platform, or
  certificate identities, or AWS tags.
- Keep Signer ARNs, names, tags, and AWS error payloads out of metric labels.

## Common Changes

- Add a new Signer metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry signing material, signed payloads,
  or signing-parameter values, leave it out of the scanner contract.
- Add new relationship evidence only when the Signer API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape (ARN-equality for ACM certificates, the bare platform id for
  the signing platform).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not start signing jobs, read signing material, read signed objects, or call
  any Signer mutation/signing API.
- Do not persist signing-parameter values, revocation records, or per-job data.
- Do not resolve Signer names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
