# AGENTS.md - internal/collector/awscloud/services/ram guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned RAM domain types.
3. `scanner.go` - resource-share, permission, and relationship emission.
4. `relationships.go` - relationship target-type, join-key, and principal
   classification.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `../organizations/scanner.go` - the account-id and OU/organization ARN
   resource-id conventions the RAM principal edges join to.
7. `docs/public/services/collector-aws-cloud-scanners.md` - RAM slice
   requirements.

## Invariants

- Keep RAM API access behind `Client`; do not import the AWS SDK into this
  package.
- Never persist a permission policy document body. The `Permission` type must
  not declare a policy field, and the adapter must never call `GetPermission`.
- Emit reported evidence only. Do not infer environment, workload ownership, or
  deployable-unit truth from share names, resource ARNs, or tags.
- Every relationship must set a non-empty `target_type` matching the target
  scanner's `resource_id` form. Do not emit empty target types.
- Principal-account edges target `aws_organizations_account` by bare account id
  (no target ARN). OU edges target `aws_organizations_organizational_unit` by
  OU ARN. Organization/root edges target `aws_organizations_root` by ARN.
- Classify principal ids by their form, never by a hardcoded `arn:aws:` prefix.
- Deduplicate permission resources by ARN across shares.
- Wrap client errors with `%w`; never swallow partial failures.

## Common Changes

- Add a new RAM relationship by extending the scanner-owned type, writing a
  focused scanner test first, then mapping it through `awscloud` envelope
  builders with a non-empty target type and join key.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add a permission policy document body field or a `GetPermission` call.
- Do not enumerate shares owned by other accounts (resource owner
  OTHER-ACCOUNTS) without a scope and performance review.
- Do not resolve RAM shares or shared resources to source repositories here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
