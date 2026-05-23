# AGENTS.md - internal/collector/awscloud/services/rds guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned RDS domain types.
3. `scanner.go` - resource emission for instances, clusters, and subnet groups.
4. `relationships.go` - direct RDS relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/docs/adrs/2026-04-20-aws-cloud-scanner-collector.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep RDS API access behind `Client`; do not import the AWS SDK into this
  package.
- Never connect to a database, read snapshot payloads, read log contents, read
  Performance Insights samples, discover schemas or tables, or mutate RDS
  resources.
- Never persist database names, master usernames, passwords, connection
  secrets, snapshot identifiers, log payloads, schemas, tables, or row data.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from endpoints, names, tags, or account
  aliases.
- Preserve stable RDS identities across repeated observations in the same AWS
  generation.
- Keep endpoints, ARNs, resource names, tags, KMS key IDs, and parameter group
  names out of metric labels.

## Common Changes

- Add a new RDS metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when RDS directly reports both sides and
  the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk` adapter,
  not here.

## What Not To Change Without An ADR

- Do not connect to RDS engines, read data-plane metadata, read logs, inspect
  snapshots, mutate resources, or add graph writes.
- Do not resolve endpoint names, tags, or subnet group names into workload
  ownership here; correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
