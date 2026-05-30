# AGENTS.md - internal/collector/awscloud/services/transfer guidance

## Read First

1. `README.md` - package purpose, exported surface, exclusion policy, and
   invariants.
2. `types.go` - scanner-owned Transfer domain types.
3. `scanner.go` - server and user resource emission.
4. `relationships.go` - relationship emission rules and partition-aware ARN
   synthesis.
5. `helpers.go` - ARN gating, partition derivation, and home-directory path
   parsing.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Transfer API access behind `Client`; do not import the AWS SDK into this
  package.
- Never create, update, delete, start, or stop a server or user, and never wire
  any `Create*`, `Update*`, `Delete*`, `Start*`, `Stop*`, `Import*`, `Test*`, or
  other mutation/key-material API.
- Never persist host key fingerprints, host key material, SSH public key bodies,
  user policy JSON, POSIX UID/GID material, login banners, or identity-provider
  invocation secrets. The scanner-owned types have no field for them.
- Record home-directory mappings as paths only. Never read object or file
  contents.
- Use server and user ARNs from the API directly. Prefer API-provided ARNs for
  ACM certificate and IAM role targets. Synthesize the S3 bucket and EFS file
  system home-directory target ARNs partition-aware via `partition(boundary)`;
  never hardcode `arn:aws:`.
- Key VPC endpoint and Elastic IP edges by the bare ID the VPC scanner
  publishes; do not set a target ARN on those edges.
- Emit ARN-keyed edges only when AWS reports an ARN-shaped (or synthesizable)
  join key. Skip the EFS home-directory edge when the boundary account or region
  is unknown.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from server, user, or path
  names, or AWS tags.
- Preserve stable server and user identities across repeated observations in the
  same AWS generation.
- Keep Transfer resource ARNs, names, paths, tags, and AWS error payloads out of
  metric labels.

## Common Changes

- Add a new Transfer metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry key, policy, or credential material,
  leave it out of the scanner contract until an ADR documents a sanitized
  exception.
- Add new relationship evidence only when the Transfer API reports both sides
  directly and the target identity matches how the target scanner publishes its
  `resource_id` (bare ID for VPC endpoint/EIP, ARN for ACM/IAM/log group, and
  synthesized partition-aware ARN for S3/EFS).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not put events, mutate servers, mutate users, import or read host keys,
  import or read SSH public keys, or call any Transfer mutation or
  key-material API.
- Do not persist user policy JSON, POSIX UID/GID material, login banners, or
  identity-provider invocation secrets.
- Do not resolve Transfer names, paths, or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
