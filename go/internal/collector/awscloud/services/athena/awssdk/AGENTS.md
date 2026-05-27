# AGENTS.md - internal/collector/awscloud/services/athena/awssdk guidance

## Read First

1. `README.md` - package purpose, telemetry, and invariants.
2. `client.go` - Athena SDK pagination, safe metadata mapping, and telemetry.
3. `mapper.go` - WorkGroup, DataCatalog, and NamedQuery field mapping with SQL
   body discard.
4. `../scanner.go` - scanner-owned Athena fact selection.
5. `../README.md` - Athena scanner contract.
6. `../../../README.md` - AWS cloud envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Athena SDK calls here, not in `cmd/collector-aws-cloud` or the scanner
  package.
- Wrap each AWS paginator page or point read in `recordAPICall`.
- Keep metric labels bounded to service, account, region, operation, and
  result.
- Persist only safe workgroup, data catalog, prepared-statement, named-query,
  and tag metadata.
- Never extend the `apiClient` interface to include StartQueryExecution,
  StopQueryExecution, GetQueryExecution, GetQueryResults, ListQueryExecutions,
  GetNamedQuery, CreateNamedQuery, DeleteNamedQuery, UpdateNamedQuery,
  CreatePreparedStatement, UpdatePreparedStatement, DeletePreparedStatement,
  GetPreparedStatement, or any other Athena API that returns SQL bodies,
  query result rows, or query history strings.
- Always discard `QueryString` from `BatchGetNamedQuery` responses before
  returning to the scanner.
- Do not cache AWS credentials or SDK clients beyond the claim-scoped runtime
  object that created this adapter.

## Common Changes

- Add a new Athena metadata read by extending `Client`, writing a scanner or
  adapter test first, then mapping the SDK response into scanner-owned types.
- Add a new throttle code in `isThrottleError` only after AWS or Smithy
  evidence shows the code is retry/throttle-shaped.
- Extend workgroup or data catalog mapping only for AWS source data that is
  metadata and does not reveal query bodies, query results, policy JSON, or
  secret material.

## What Not To Change Without An ADR

- Do not call any Athena Get API that returns the SQL body
  (`GetNamedQuery`, `GetPreparedStatement`).
- Do not call Athena mutation APIs (Create/Update/Delete on named queries,
  prepared statements, workgroups, or data catalogs).
- Do not infer workload, environment, deployment, or ownership truth from
  workgroup names, data catalog names, tags, or named-query metadata.
- Do not write facts, graph rows, workflow rows, or reducer-owned state here.
