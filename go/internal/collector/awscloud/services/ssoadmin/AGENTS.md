# AGENTS.md - internal/collector/awscloud/services/ssoadmin guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Identity Center domain types.
3. `scanner.go` and `observations.go` - resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and data boundaries.

## Invariants

- Keep sso-admin and identitystore API access behind `Client`; do not import
  the AWS SDK into this package.
- NEVER persist permission set inline policy bodies or permissions boundary
  bodies. They encode the org least-privilege model and live in IAM.
- NEVER persist customer-managed policy bodies. Reference customer-managed
  policies by name and path only.
- AWS managed policies are ARN references only; bodies stay in IAM.
- NEVER persist application access-scope attributes; they can carry sensitive
  group filters.
- Redact principal display names through `awscloud.RedactString`. Read only the
  identity store `DisplayName`; never read addresses, emails, phone numbers,
  birthdate, structured name, or memberships.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from names, relay state, or tags.
- Preserve stable identities across repeated observations in the same AWS
  generation. Account assignments key on permission-set-ARN, account, and
  principal because Identity Center exposes no assignment ID.
- Keep ARNs, principal IDs, relay state, and tags out of metric labels.

## Common Changes

- Add a new Identity Center metadata field by extending the relevant
  scanner-owned type, writing a focused scanner or adapter test first, then
  mapping it through `awscloud` envelope builders.
- Add new relationship evidence only when sso-admin reports both sides
  directly.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read inline policy bodies, permissions boundary bodies, customer-managed
  policy bodies, or application access-scope attributes.
- Do not add identity store membership listing or structured identity reads.
- Do not resolve names, relay state, or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
