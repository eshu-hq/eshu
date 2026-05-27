# AGENTS.md - internal/collector/awscloud/services/organizations guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Organizations domain types.
3. `scanner.go` - resource, relationship, warning, and redaction emission.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime and
   credential requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner coverage.

## Invariants

- Keep Organizations API access behind `Client`; do not import the AWS SDK into
  this package.
- Never call Organizations mutation APIs.
- Never persist policy document bodies, statements, conditions, action lists,
  NotAction values, or guardrail text.
- Redact account email and account name values with the shared AWS redaction
  helper before persistence.
- Do not store raw account names in resource names or relationship attributes.
- Emit reported evidence only. Do not infer environment, deployment, workload,
  ownership, or deployable-unit truth from account names, OU names, tags, or
  policy bindings.
- Preserve stable root, OU, account, policy, and delegated-admin identities
  across repeated observations in the same AWS generation.
- Keep policy IDs, account names, account emails, tags, and ARNs out of metric
  labels.

## Common Changes

- Add a new Organizations metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add relationship evidence only when the Organizations API reports both sides
  directly and the target identity is not sensitive.
- Extend SDK pagination in the `awssdk` adapter, not here.
- If a future security-reviewed opt-in persists policy bodies, add a separate
  explicit contract, tests, docs, redaction/security review, and operator
  configuration. Do not broaden this default scanner path.

## What Not To Change Without An ADR

- Do not add policy body persistence or policy analysis here.
- Do not add account lifecycle mutations, policy mutations, delegated-admin
  mutations, or service-access mutations.
- Do not resolve Organizations names or policies into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
