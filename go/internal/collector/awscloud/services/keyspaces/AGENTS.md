# AGENTS.md - internal/collector/awscloud/services/keyspaces guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Keyspaces domain types.
3. `scanner.go` - keyspace and table resource emission.
4. `relationships.go` - table-in-keyspace and direct KMS relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep Keyspaces API access behind `Client`; do not import the AWS SDK into this
  package.
- Never execute CQL, run `ExecuteStatement`, `BatchStatement`, or `Select`, read
  table rows or cells, restore tables, or mutate keyspaces or tables.
- Schema column names, data types, partition keys, clustering keys, and static
  column names are structural metadata and are safe to report. Row data and cell
  values are not.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from keyspace names, table names, tags, or
  schema columns.
- Derive the keyspace ARN partition from the keyspace or table ARN, never a
  hardcoded `arn:aws:` literal, so GovCloud and China edges resolve.
- Emit the KMS edge only when Keyspaces reports a customer-managed key
  identifier; set `target_arn` only for ARN-shaped identifiers.
- Keep keyspace names, table names, ARNs, tags, schema columns, KMS key IDs, and
  raw AWS error payloads out of metric labels.

## Common Changes

- Add a new Keyspaces metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Keyspaces directly reports both sides
  and the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not add Keyspaces data-plane calls, CQL execution, row/cell reads,
  RestoreTable calls, mutations, or graph writes.
- Do not resolve keyspace names, table names, tags, or schema columns into
  workload ownership here; correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
