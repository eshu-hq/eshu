# AGENTS.md - internal/collector/awscloud/services/redshift guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Redshift and Redshift Serverless domain types.
3. `scanner.go` - resource emission for clusters, parameter groups, subnet
   groups, snapshots, scheduled actions, Serverless namespaces, and Serverless
   workgroups.
4. `relationships.go` - direct Redshift relationship evidence.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Redshift and Redshift Serverless API access behind `Client`; do not
  import the AWS SDK into this package.
- Never connect to a Redshift warehouse, run queries, read snapshot contents,
  read table or row data, fetch master user passwords or admin passwords, or
  call any Redshift / Redshift Serverless mutation API.
- Do not persist master user names, admin user names, master password secret
  ARNs, master password KMS key IDs, query results, snapshot payloads, table
  data, or IAM/resource policy JSON.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from endpoints, names, tags, or account
  aliases.
- Preserve stable Redshift identities across repeated observations in the same
  AWS generation. Cluster ARNs are synthesized from `(region, account_id,
  cluster_identifier)`; parameter group and subnet group ARNs are synthesized
  from `(region, account_id, name)` in the adapter so reducers see consistent
  identity across resource and relationship facts.
- Keep endpoints, ARNs, resource names, tags, KMS key IDs, parameter group
  names, namespace names, and workgroup names out of metric labels.
- Distinguish provisioned and Serverless resources through the emitted
  `resource_type`, never by widening the `service_kind` value. Both surfaces
  use `service_kind="redshift"`.

## Common Changes

- Add a new Redshift metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when Redshift directly reports both sides
  and the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk`
  adapter, not here.

## What Not To Change Without An ADR

- Do not open warehouse connections, run queries, read table or column data,
  read snapshot contents, inspect raw scheduled-action JSON payloads, mutate
  Redshift resources, or add graph writes.
- Do not resolve endpoint names, tags, namespace names, workgroup names, or
  subnet group names into workload ownership here; correlation belongs in
  reducers.
- Do not add AWS credential loading or STS calls to this package.
