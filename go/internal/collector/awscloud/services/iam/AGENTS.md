# AGENTS.md - internal/collector/awscloud/services/iam guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned IAM domain types.
3. `scanner.go` - IAM resource and relationship emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - IAM slice
   requirements.

## Invariants

- Keep IAM API access behind `Client`; do not import the AWS SDK into this
  package.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  or deployable-unit truth from IAM names or policy text.
- Preserve stable role, user, policy, profile, relationship, and permission
  identities across repeated observations in the same AWS generation.
- Keep trust policy JSON out of facts, metric labels, logs, status errors, and
  graph properties. ARNs may remain provider-native source identity in facts.
- Derived `aws_iam_permission` facts are metadata-only. Emit only the normalized
  statement (effect, action set, resource pattern, condition key/operator names,
  trust assume-principals). NEVER persist the raw policy JSON body or condition values.
  The SDK adapter normalizes documents; this package consumes `PolicyStatement`
  values and never holds raw JSON.
- `secrets_iam_posture` facts are source evidence only. Keep
  `collector_kind=secrets_iam_posture` and do not reuse AWS cloud envelope
  helpers for those fact kinds.
- Do not project the permission facts into graph edges here. The CAN_ASSUME /
  escalation-primitive reducer projection is a separate principal-review PR
  (issue #1134).

## Common Changes

- Add a new IAM resource by extending the scanner-owned type, writing a focused
  scanner test first, then mapping it through `awscloud` envelope builders.
- Add a new IAM relationship by defining the relationship constant in
  `awscloud`, adding scanner coverage, and keeping source and target identity
  explicit.
- Add a new derived permission attribute by extending `PolicyStatement` and the
  `awscloud.IAMPermissionObservation` builder, keeping it metadata-only.
- Extend SDK pagination and policy-document fan-out (with a per-principal bound)
  in the runtime adapter, not here.

## What Not To Change Without An ADR

- Do not turn IAM trust principals into canonical identity truth here.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
