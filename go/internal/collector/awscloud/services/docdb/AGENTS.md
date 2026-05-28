# AGENTS.md - internal/collector/awscloud/services/docdb guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned DocumentDB domain types and the `Client` contract.
3. `scanner.go` - resource emission for clusters, instances, parameter groups,
   snapshots, subnet groups, global clusters, and event subscriptions.
4. `relationships.go` - direct DocumentDB relationship evidence.
5. `contract_test.go` - the load-bearing mutation/data-plane exclusion proof.
6. `../rds/README.md` - the RDS-shaped scanner this package mirrors.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep DocumentDB API access behind `Client`; do not import the AWS SDK into
  this package. The `Client` method set is metadata-only by contract.
- Never connect to a cluster, read documents or collections, read snapshot
  contents, read cluster parameter values, or mutate any DocumentDB resource.
- Never persist master user passwords, master user secrets, database document
  contents, collections, indexes, or cluster parameter values.
- Cluster parameter groups persist name, family, and parameter count only.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from endpoints, names, tags, or account
  aliases.
- Preserve stable DocumentDB identities across repeated observations in the
  same AWS generation.
- Keep endpoints, ARNs, resource names, tags, KMS key IDs, and parameter group
  names out of metric labels.

## Common Changes

- Add a new DocumentDB metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when DocumentDB directly reports both
  sides and the target identity is not secret.
- Extend SDK pagination and optional-not-found handling in the `awssdk`
  adapter, not here.
- When adding a `Client` method, update `contract_test.go` so the new
  metadata-only read is in the allowed set and no mutation verb leaks in.

## What Not To Change Without An ADR

- Do not connect to DocumentDB engines, read documents, read parameter values,
  inspect snapshot contents, mutate resources, or add graph writes.
- Do not resolve endpoint names, tags, or subnet group names into workload
  ownership here; correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
