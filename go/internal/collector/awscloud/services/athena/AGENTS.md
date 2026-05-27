# AGENTS.md - internal/collector/awscloud/services/athena guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned Athena domain types.
3. `scanner.go` - workgroup, data catalog, prepared-statement, named-query,
   and relationship emission.
4. `relationships.go` - workgroup result-bucket, workgroup KMS, prepared
   statement workgroup, and named-query workgroup relationship builders.
5. `../../README.md` - shared AWS cloud observation and envelope contract.
6. `docs/public/services/collector-aws-cloud.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Athena API access behind `Client`; do not import the AWS SDK into this
  package.
- Never start, stop, or mutate queries. The package must never reference
  `StartQueryExecution`, `StopQueryExecution`, `GetQueryResults`,
  `GetQueryExecution`, `ListQueryExecutions`, `CreateNamedQuery`,
  `DeleteNamedQuery`, `UpdateNamedQuery`, `CreatePreparedStatement`,
  `UpdatePreparedStatement`, `DeletePreparedStatement`, or `GetPreparedStatement`.
- Never persist named-query SQL bodies, prepared-statement query statements,
  query result rows, query execution result location object contents, or query
  history strings.
- Workgroup result-bucket relationships must use the ARN-only S3 bucket form;
  do not include object keys, prefixes, or any object content metadata in the
  relationship payload.
- Workgroup KMS relationships should leave `target_arn` empty when AWS returns
  an alias or key ID rather than a full ARN; only ARN-shaped identifiers
  populate `target_arn`.
- Named-query and prepared-statement relationships only flow when AWS reports
  a workgroup name.
- Preserve stable workgroup, data catalog, prepared-statement, and named-query
  identities across repeated observations in the same AWS generation.
- Keep workgroup ARNs, data catalog ARNs, prepared-statement names,
  named-query IDs, named-query names, database names, tags, and engine version
  strings out of metric labels.

## Common Changes

- Add a new Athena metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders.
- Add a new relationship only when the Athena API reports both sides directly
  and the target identity is not sensitive.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not start, stop, mutate, or replay Athena queries.
- Do not resolve named queries, prepared statements, workgroup tags, or
  database names into workload ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
- Do not call any Athena Get API that returns the SQL body (`GetNamedQuery`,
  `GetPreparedStatement`).
