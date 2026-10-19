# AGENTS.md — Postgres tfstate family guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `drift_evidence.go` for the loader, `backend_facts.go`
   for the readers, `../compat_tfstate.go` for the root compat surface.

## Invariants

- Keep the package clause as `package tfstate`; callers import the
  `storage/postgres/tfstate` path without an alias.
- Never import the parent `postgres` package from here (import cycle).
- Keep all `*_sql.go` constants byte-identical on moves; predicate or
  column changes need their own issue with EXPLAIN and cardinality proof
  per `eshu-postgres-rigor`.
- Keep `PostgresTerraformBackendQuery`, `TerraformStateBackendFactReader`,
  `TerraformStatePriorSnapshotReader`,
  `TerraformStateGitReadinessChecker`, `PostgresDriftEvidenceLoader`,
  `UnresolvedRecorder`, `BuildModulePrefixMap`, `LoadConfigByAddress`,
  and `CoerceJSONString` exported until the root AWS consumer and the
  cmd/ wiring sites move; the root aliases in `../compat_tfstate.go`
  retire entry by entry.
- Keep `cleanFactKinds` identical to the root's copy in
  `../facts_filtered.go`, and the parser-shape fixtures identical to the
  root's `../runtime_drift_config_fixtures_test.go`, until one side is
  hoisted or retired.
- `flattenStateAttributes` must stay behavior-identical to the parser's
  `ctyValueToDriftString`; the cross-package encoding test pins this.

## Common changes

- New loader inputs need a `db.Queryer`-shaped field (never `*sql.DB`
  directly) so unit tests keep staging `fakeExecQueryer` FIFO responses.
- Live-Postgres tests set up schema via `tfstateMigrationSQL` (shipped
  `migrations` bytes, never a hand copy) and adapt `*sql.DB` with
  `tfstateSQLDB`.

## Failure modes

- `LoadDriftEvidence` with a nil DB fails closed; blank scope or anchor
  IDs are rejected before any query.
- Undecodable payload rows are skipped with a WARN log, not a load
  failure; watch for log spikes indicating upstream schema drift.
- A query with no staged FIFO response fails the test loudly
  ("unexpected query") — stage every statement the path issues, in order.
