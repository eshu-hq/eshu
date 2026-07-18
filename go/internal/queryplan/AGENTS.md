# AGENTS.md - internal/queryplan guidance for LLM assistants

## Read first

1. `README.md` - package purpose, fixture contract, and evidence notes.
2. `doc.go` - godoc boundary and non-runtime behavior.
3. `validator.go` - manifest schema and static validation rules.
4. `source_coverage.go` - production query-call discovery and fail-closed
   inventory rules.

## Invariants

- This package is static validation only. Do not add live Neo4j, NornicDB,
  Postgres, provider, or network calls here.
- Keep source paths generic repo-relative paths. Do not put private hosts,
  local machine paths, IPs, credentials, or customer details into fixtures.
- When a read path is backed by SQL/read-model evidence rather than Cypher,
  mark it as `query_kind: sql_read_model` and include a caveat.
- New Cypher hot paths must declare source owner, anchor labels/properties,
  schema evidence names, bounds, ordering requirements, and bad plan signatures.
- Every non-test `Run` or `RunSingle` call under `internal/query` must appear in
  `testdata/query-source-coverage.yaml` with the exact enclosing symbol and call
  count. Link hot calls through `entry_ids`; use `non_hot_reason` only for an
  explicitly reviewed inventory-only support or bounded read.
- Keep live `PROFILE` tests in `internal/query`, never in this package.

## Verification

Run:

```bash
go test ./internal/queryplan -count=1
scripts/verify-package-docs.sh
```
