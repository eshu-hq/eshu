# AGENTS.md - internal/collector/awscloud/services/iam guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned IAM domain types.
3. `scanner.go` - IAM resource and relationship emission.
4. `../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - IAM slice
   requirements.

## Invariants

- Keep IAM API access behind `Client`; do not import the AWS SDK into this
  package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  or deployable-unit truth from IAM names or policy text.
- Preserve stable role, policy, profile, and relationship identities across
  repeated observations in the same AWS generation.
- Keep trust policy JSON and ARNs out of metric labels.

## Common Changes

- Add a new IAM resource by extending the scanner-owned type, writing a focused
  scanner test first, then mapping it through `awscloud` envelope builders.
- Add a new IAM relationship by defining the relationship constant in
  `awscloud`, adding scanner coverage, and keeping source and target identity
  explicit.
- Extend SDK pagination in the runtime adapter, not here.

## What Not To Change Without An ADR

- Do not turn IAM trust principals into canonical identity truth here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
