# AGENTS.md - internal/collector/awscloud/services/backup guidance

## Read First

1. `README.md` - package purpose, exported surface, security invariants.
2. `types.go` - scanner-owned Backup domain types.
3. `scanner.go` - Backup resource and relationship emission orchestration.
4. `observations.go` - per-resource observation builders.
5. `relationships.go` - relationship emission helpers.
6. `contract_test.go` - asserts the Client interface excludes mutation APIs.
7. `../../README.md` - shared AWS cloud observation and envelope contract.

## Invariants

- Keep AWS Backup API access behind `Client`; do not import the AWS SDK into
  this package.
- NEVER read recovery point contents.
- NEVER persist backup vault access policy JSON bodies.
- NEVER read or persist recovery-point restore metadata values.
- NEVER persist framework control input parameter VALUES.
- NEVER expose a Create/Update/Delete/Start verb on the `Client` interface.
- Emit reported evidence only. Do not infer ownership, deployment, or
  workload truth from selection tag conditions or framework scopes.
- Keep ARNs, names, and tags out of metric labels.

## Common Changes

- Add a new Backup metadata field by extending the relevant scanner-owned
  record, writing a focused scanner or adapter test first, then mapping it
  through `awscloud` envelope builders.
- Add a new relationship type only when AWS Backup reports both sides
  directly.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not start, copy, or delete backup jobs.
- Do not call `GetBackupVaultAccessPolicy`, `GetRecoveryPointRestoreMetadata`,
  or any other API that can carry policy bodies or source-resource
  configuration values.
- Do not resolve selection tag patterns into workload truth here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
