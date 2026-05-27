# AGENTS.md - internal/collector/awscloud/services/securityhub guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Security Hub domain types.
3. `scanner.go` - hub, standard, control, member, insight, action target, and
   finding aggregate fact emission.
4. `awssdk/README.md` - AWS SDK API allowlist and redaction boundary.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Security Hub API access behind `Client`; do not import the AWS SDK into
  this package.
- Never call or model mutation APIs: BatchUpdateFindings, BatchImportFindings,
  CreateInsight, DeleteInsight, UpdateInsight, EnableSecurityHub,
  DisableSecurityHub, EnableStandards, DisableStandards, CreateActionTarget,
  DeleteActionTarget, UpdateActionTarget, BatchEnableStandards, or
  BatchDisableStandards.
- Never persist finding bodies. Resource IDs, resource details, remediation
  text, product fields, user-defined fields, note text, network details, and
  process details are out of scope.
- Never persist insight filter expressions.
- Keep finding posture as aggregates only. Do not add finding IDs, resource
  ARNs, product-field keys, or user-defined values to facts or metric labels.
- Redact custom action target descriptions with `awscloud.RedactString` before
  fact emission.
- Emit reported evidence only. Do not infer AWS Organizations hierarchy,
  workload ownership, deployment truth, or reducer-owned finding truth.
- Keep Security Hub ARNs, tags, finding IDs, insight filters, and resource
  selectors out of metric labels.

## Common Changes

- Add a new Security Hub metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Security Hub reports both sides
  directly and neither side depends on finding-body details.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not persist finding details, insight filters, automation rules, resource
  details, remediation instructions, notes, process details, or network details.
- Do not introduce reducer finding admission, graph writes, or query behavior.
- Do not resolve account IDs, tags, standards, controls, or insight names into
  ownership or deployment truth here; reducers own correlation.
- Do not add AWS credential loading or STS calls to this package.
