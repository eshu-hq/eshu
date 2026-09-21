# AGENTS.md - services/backup/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and forbidden APIs.
2. `client.go` - AWS Backup SDK pagination and per-call telemetry.
3. `mapping.go` - projection of SDK records into scanner-owned types.
4. `../scanner.go` - scanner-owned Backup fact selection.
5. `../README.md` - Backup scanner contract.

## Invariants

- Keep AWS Backup SDK calls here, not in `cmd/collector-aws-cloud` or the
  scanner package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- NEVER add Create/Update/Delete or Start verbs to `apiClient`.
- NEVER call `GetBackupVaultAccessPolicy`,
  `GetRecoveryPointRestoreMetadata`, or any API that returns vault access
  policy bodies or source-resource configuration values.
- Project SDK responses into scanner-owned types; never persist raw SDK
  values outside the `mapping.go` projection layer.

## Common Changes

- Add a new safe metadata read by extending `backup.Client`, writing a scanner
  or adapter test first, then mapping the SDK response into scanner-owned
  types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend mapping helpers only for AWS source data that does not reveal
  policy bodies, restore metadata, or framework control input parameter
  values.

## What Not To Change Without An ADR

- Do not start, copy, or delete backup jobs.
- Do not call APIs that surface vault access policy bodies or recovery point
  restore metadata.
- Do not infer workload, environment, deployment, or ownership truth from
  Backup names, ARNs, or tags.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
