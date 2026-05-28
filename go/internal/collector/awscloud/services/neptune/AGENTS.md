# AGENTS.md - internal/collector/awscloud/services/neptune guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Neptune domain types and the `Client` contract.
3. `scanner.go` - resource emission orchestration; `observations.go` - the
   per-resource observation builders.
4. `relationships.go` - direct Neptune relationship evidence.
5. `contract_test.go` - the load-bearing mutation/graph-data-plane exclusion
   proof.
6. `../docdb/README.md` and `../rds/README.md` - the RDS-shaped scanners this
   package mirrors.
7. `../../README.md` - shared AWS cloud observation and envelope contract.
8. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Neptune API access behind `Client`; do not import the AWS SDK into this
  package. The `Client` method set is metadata-only by contract and excludes
  every Neptune Analytics graph data-plane call (ExecuteQuery, CancelQuery,
  GetQuery, ListQueries) and every mutation.
- Never connect to a cluster or graph endpoint, run a graph query, read graph
  vertex or edge contents, read snapshot contents, read cluster parameter
  values, or mutate any Neptune resource.
- Never persist master user passwords, master user secrets, graph vertex or
  edge contents, `ExecuteQuery` results, or cluster parameter values.
- Cluster parameter groups persist name and family only.
- Neptune Analytics graphs persist name, status, vector-search dimension, and
  provisioning shape only.
- Emit reported evidence only. Do not infer deployment, workload, repository,
  ownership, or deployable-unit truth from endpoints, names, tags, or account
  aliases.
- Preserve stable Neptune identities across repeated observations in the same
  AWS generation.
- Keep endpoints, ARNs, resource names, tags, KMS key IDs, and parameter group
  names out of metric labels.

## Common Changes

- Add a new Neptune metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add new relationship evidence only when Neptune directly reports both sides
  and the target identity is not secret. Set a non-empty `target_type` that
  matches the target scanner's resource type.
- Extend SDK pagination and optional-not-found handling in the `awssdk`
  adapter, not here.
- When adding a `Client` method, update `contract_test.go` so the new
  metadata-only read is in the allowed set and no mutation or graph data-plane
  verb leaks in.

## What Not To Change Without An ADR

- Do not connect to Neptune engines or graph endpoints, run graph queries, read
  vertex/edge data, read parameter values, inspect snapshot contents, mutate
  resources, or add Eshu graph writes.
- Do not resolve endpoint names, tags, or subnet group names into workload
  ownership here; correlation belongs in reducers.
- Do not add AWS credential loading or STS calls to this package.
